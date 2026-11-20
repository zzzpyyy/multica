import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Attention, Issue } from "../api";
import { buildTree, catalog, EMPTY_RUNS, indexAttention, names, shownStatus, waitingOn, type Ctx } from "../model";
import { Board } from "./Board";
import { MessageBody } from "./Chat";
import { WorkspaceContext, type WorkspaceData } from "./context";

// BUNN-31 (一鸣 11-20) on layout A: the stage holding the selected issue
// stays open (问题 2), every row writes its status (观察 1), a sub-issue's
// title wraps (问题 4), a cut name shows in full on hover (观察 2), and a
// no_response turn reads in Chinese.
let seq = 0;
const issue = (id: string, status: string, extra: Partial<Issue> = {}): Issue => {
  seq += 1;
  return {
    id, number: seq, identifier: `NQC-${seq}`, title: id, description: null, status, assignee_type: "agent", assignee_id: "a-tl",
    parent_issue_id: null, project_id: null, stage: null, created_at: `2026-11-20T10:00:${String(seq).padStart(2, "0")}Z`, ...extra,
  };
};

function page(list: Issue[], currentId: string, attention: Partial<Attention> = {}) {
  const tree = buildTree(list);
  const ctx: Ctx = {
    cat: catalog([]),
    att: indexAttention({ awaiting_reply: [], in_review: [], in_review_by_others: [], ...attention }),
    runs: EMPTY_RUNS,
    names: names([{ user_id: "u-me", name: "qa31-member-long-name", email: "z@example.test" }], [{ id: "a-tl", name: "TL" }], [], []),
  };
  const ws = { ...ctx, slug: "nest-qa3", workspace: { id: "w", slug: "nest-qa3" }, userId: "u-me", tree, ready: true, current: tree.byId.get(currentId)! } as WorkspaceData;
  const html = renderToStaticMarkup(
    <QueryClientProvider client={new QueryClient()}>
      <WorkspaceContext.Provider value={ws}>
        <Board current={ws.current} commentId={null} filter={{ text: "", mine: false }} />
      </WorkspaceContext.Provider>
    </QueryClientProvider>,
  );
  return { html, ctx, tree };
}
const text = (html: string) => html.replace(/<[^>]+>/g, "");
const folded = (html: string) => html.match(/<button type="button" class="folded-row" aria-expanded="(true|false)">(.*?)<\/button>/);

describe("the selected issue's stage stays open (问题 2)", () => {
  seq = 0;
  // NQC-160's stage 1 holds only NQC-161, done; stage 2 is open.
  const list = [issue("P", "in_progress"), issue("D", "done", { parent_issue_id: "P", stage: 1 }), issue("T", "todo", { parent_issue_id: "P", stage: 2 })];

  it("opened by address: its row and detail are there, the stage is not folded", () => {
    const { html } = page(list, "D");
    expect(html).toContain('data-child="NQC-2"');
    expect(html).toContain('data-issue="NQC-2"');
    // The folded line stays as the stage's header, open, like 已完成.
    expect(folded(html)?.[1]).toBe("true");
  });

  it("another issue selected: the closed stage folds as before", () => {
    const { html } = page(list, "T");
    expect(folded(html)?.[1]).toBe("false");
    expect(text(folded(html)![2]!)).toBe("阶段 1 · 已完成 1 个");
    expect(html).not.toContain('data-child="NQC-2"');
  });

  it("approved in place (in_review, then done): still open, the detail stays", () => {
    seq = 0;
    const before = [issue("P", "in_progress"), issue("R", "in_review", { parent_issue_id: "P", stage: 1 })];
    const after = before.map((i) => (i.id === "R" ? { ...i, status: "done" } : i));
    for (const l of [before, after]) {
      const { html } = page(l, "R", l === before ? { in_review: [{ issue_id: "R", ancestor_ids: ["P"] }] } : {});
      expect(html).toContain('data-child="NQC-2"');
      expect(html).toContain('data-issue="NQC-2"');
      if (l === after) expect(folded(html)?.[1]).toBe("true");
    }
  });
});

describe("rows (观察 1, 观察 2, 问题 4)", () => {
  seq = 0;
  const list = [
    issue("P", "in_progress"),
    issue("A", "todo", { parent_issue_id: "P", stage: 1 }),
    issue("B", "in_progress", { parent_issue_id: "P", stage: 1 }),
    issue("C", "backlog", { parent_issue_id: "P", stage: 1 }),
    issue("E", "blocked", { parent_issue_id: "P", stage: 1 }),
    issue("R", "in_review", { parent_issue_id: "P", stage: 1, assignee_type: "member", assignee_id: "u-me" }),
  ];
  const { html, ctx, tree } = page(list, "P", { in_review: [{ issue_id: "R", ancestor_ids: ["P"] }] });
  const state = (identifier: string) => html.match(new RegExp(`data-child="${identifier}".*?<span class="state"[^>]*>(.*?)</span>`))?.[1];

  it("writes the status, so 待办 and 进行中 differ without color", () => {
    expect(state("NQC-2")).toBe("待办");
    expect(state("NQC-3")).toBe("进行中");
    expect(state("NQC-6")).toBe("审核中");
    expect(html).toMatch(/<span class="dot[^"]*" aria-hidden="true"><\/span>/);
  });

  it("never says the same thing twice on a row", () => {
    // 待规划 is already 在等谁; 阻塞，等你处理 already says blocked.
    expect(state("NQC-4")).toBe("");
    expect(state("NQC-5")).toBe("");
    for (const i of tree.byId.values()) {
      const w = waitingOn(i, ctx);
      const s = shownStatus(i, w, ctx.cat);
      if (s && w) expect(w.includes(s) || s.includes(w)).toBe(false);
    }
  });

  it("a cut name has its full name on hover", () => {
    expect(html).toContain('<span class="who-name" title="qa31-member-long-name">qa31-member-long-name</span>');
  });

  it("a sub-issue's title wraps, a top-level one stays one line", () => {
    const css = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
    expect(css).toMatch(/\.open \.title \{ overflow: hidden; text-overflow: ellipsis; white-space: nowrap; \}/);
    expect(css).toMatch(/\.node-row:not\(\.root\) \.title \{ overflow: visible; white-space: normal;/);
  });
});

describe("no_response", () => {
  it("shows the Chinese line instead of the stored English fallback", () => {
    const html = renderToStaticMarkup(<MessageBody message={{ content: "The agent finished this turn without a text reply.", message_kind: "no_response" }} />);
    expect(html).toContain("本轮已结束，super-agent 没有返回文字回复。");
    expect(html).not.toContain("The agent finished");
    expect(renderToStaticMarkup(<MessageBody message={{ content: "你好", message_kind: "message" }} />)).toContain("你好");
    expect(renderToStaticMarkup(<MessageBody message={{ content: "你好" }} />)).toContain("你好");
  });
});
