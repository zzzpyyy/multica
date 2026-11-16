import { describe, expect, it } from "vitest";
import type { Attention, Issue, SnapshotTask } from "./api";
import {
  ancestors,
  buildTree,
  catalog,
  childrenOf,
  defaultIssue,
  EMPTY_RUNS,
  foldedLabel,
  indexAttention,
  indexRuns,
  leftGroups,
  lineText,
  names,
  oneLine,
  progress,
  runCounts,
  stageRows,
  statusText,
  stripTitle,
  treeLevel,
  waitingOn,
  type Ctx,
} from "./model";

// PRD v4 3.7 test data: P1–P6, P9; v3.4 titles for S3 and S5.
let seq = 0;
const at = (n: number) => `2026-11-16T10:00:${String(n).padStart(2, "0")}Z`;
function issue(id: string, status: string, extra: Partial<Issue> = {}): Issue {
  seq += 1;
  return {
    id,
    number: seq,
    identifier: `NEST-${seq}`,
    title: id,
    description: null,
    status,
    assignee_type: null,
    assignee_id: null,
    parent_issue_id: null,
    project_id: null,
    stage: null,
    created_at: at(seq),
    revision: 1,
    ...extra,
  };
}
const ME = "u-me";
const OTHER = "u-other";

function dataset() {
  seq = 0;
  const P1 = issue("P1", "todo");
  const list = [
    P1,
    issue("S1", "done", { parent_issue_id: "P1", stage: 1 }),
    issue("S2", "cancelled", { parent_issue_id: "P1", stage: 1 }),
    issue("S3", "in_review", { parent_issue_id: "P1", stage: 2, title: "阶段2：接口联调", assignee_type: "member", assignee_id: ME }),
    issue("S4", "blocked", { parent_issue_id: "P1", stage: 2 }),
    issue("S5", "backlog", { parent_issue_id: "P1", stage: 3, title: "NEST-1 阶段 3: 把导出报表页面的列宽和字体都调整成和设计稿一致" }),
    issue("P2", "todo"),
    issue("P3", "in_progress"),
    issue("C1", "todo", { parent_issue_id: "P3" }),
    issue("G1", "done", { parent_issue_id: "C1" }),
    issue("G2", "todo", { parent_issue_id: "C1" }),
    issue("P4", "todo"),
    issue("Q1", "done", { parent_issue_id: "P4" }),
    issue("Q2", "todo", { parent_issue_id: "P4", title: "整理文案" }),
    issue("P5", "todo"),
    issue("R1", "in_progress", { parent_issue_id: "P5" }),
    issue("R2", "in_progress", { parent_issue_id: "P5" }),
    issue("R3", "in_review", { parent_issue_id: "P5" }),
    issue("P6", "in_review"),
    issue("P9", "todo"),
  ];
  for (let k = 1; k <= 16; k++) {
    const status = k <= 13 ? "done" : k === 14 ? "blocked" : k === 15 ? "in_review" : "todo";
    list.push(issue(`K${k}`, status, { parent_issue_id: "P9", stage: k, title: `阶段 ${k}：第 ${k} 步`, assignee_type: "member", assignee_id: OTHER }));
  }
  return list;
}

/** SB4a: every in-review issue is yours, top-level ones included. */
function sb4a(list: Issue[], extra: Partial<Attention> = {}): Attention {
  const tree = buildTree(list);
  return {
    awaiting_reply: [],
    in_review: list
      .filter((i) => i.status === "in_review")
      .map((i) => ({ issue_id: i.id, ancestor_ids: ancestors(tree, i).map((a) => a.id).reverse() })),
    in_review_by_others: [],
    ...extra,
  };
}

const cat = catalog([]);
const nm = names(
  [{ user_id: ME, name: "qa", email: "qa@example.test" }, { user_id: OTHER, name: "other", email: "o@example.test" }],
  [{ id: "a-ma", name: "MA" }, { id: "a-la", name: "LA" }, { id: "a-super", name: "super-agent", system_key: "super" }],
  [{ id: "sq", name: "SQ", leader_id: "a-la" }],
  [{ id: "pj", title: "PJ" }],
);

function ctxOf(list: Issue[], att: Attention = sb4a(list), runs = EMPTY_RUNS): Ctx {
  return { cat, att: indexAttention(att), runs, names: nm };
}
const line = (list: Issue[], id: string, ctx: Ctx) => {
  const tree = buildTree(list);
  return lineText(oneLine(tree.byId.get(id)!, childrenOf(tree, id), ctx.att, cat));
};

describe("stripTitle (plan v2.3-A r3 §2.1, the six examples)", () => {
  it.each([
    ["阶段2：接口联调", "BUNN-101", "接口联调"],
    ["BUNN-101 阶段 3: 把导出报表页面的列宽和字体都调整成和设计稿一致", "BUNN-101", "把导出报表页面的列宽和字体都调整成和设计…"],
    ["BUNN-20 阶段 15：发消息时把页面选中的 Tiki 空间带给 Agent", "BUNN-20", "发消息时把页面选中的 Tiki 空间带给…"],
    ["阶段3：服务端开发（按阶段2契约）", "BUNN-26", "服务端开发（按阶段2契约）"],
    ["BUNN-20 修复", "BUNN-2", "BUNN-20 修复"],
    ["阶段 4：", "BUNN-9", "阶段 4："],
  ])("%s → %s", (title, parent, shown) => {
    expect(stripTitle(title, parent, 20)).toBe(shown);
  });

  it("the sub-issue list does not cut", () => {
    expect(stripTitle("NEST-1 阶段 3: 把导出报表页面的列宽和字体都调整成和设计稿一致", "NEST-1")).toBe(
      "把导出报表页面的列宽和字体都调整成和设计稿一致",
    );
  });

  it("counts code points", () => {
    expect(stripTitle("😀".repeat(21), null, 20)).toBe(`${"😀".repeat(20)}…`);
  });
});

describe("tree", () => {
  it("the top level is every issue whose parent is not in the list (A rule 2)", () => {
    const tree = buildTree(dataset());
    expect(tree.roots.map((r) => r.id)).toEqual(["P1", "P2", "P3", "P4", "P5", "P6", "P9"]);
    expect(ancestors(tree, tree.byId.get("G2")!).map((a) => a.id)).toEqual(["P3", "C1"]);
  });

  it("A9: a sub-issue whose parent was cleared becomes top-level", () => {
    const list = dataset().map((i) => (i.id === "S5" ? { ...i, parent_issue_id: null } : i));
    expect(buildTree(list).roots.map((r) => r.id)).toContain("S5");
  });

  it("an unknown parent or a cycle does not hang", () => {
    const a = issue("a", "todo", { parent_issue_id: "b" });
    const b = issue("b", "todo", { parent_issue_id: "a" });
    const tree = buildTree([a, b, issue("c", "todo", { parent_issue_id: "gone" })]);
    expect(tree.roots.map((r) => r.id)).toEqual(["c"]);
    expect(ancestors(tree, a).length).toBeLessThanOrEqual(32);
  });
});

describe("left column (N10, N11)", () => {
  it("N10: groups and order", () => {
    const list = dataset();
    const g = leftGroups(buildTree(list), ctxOf(list));
    expect(g.waiting.map((i) => i.id)).toEqual(["P9", "P6", "P5", "P1"]);
    expect(g.active.map((i) => i.id)).toEqual(["P4", "P3", "P2"]);
    expect(g.done).toEqual([]);
    expect(defaultIssue(g)?.id).toBe("P9");
  });

  it("N11: the one-line progress of every entry", () => {
    const list = dataset();
    const ctx = ctxOf(list);
    const tree = buildTree(list);
    expect(line(list, "P1", ctx)).toBe("阶段 2/3 · 审核中：接口联调 · 1 个阻塞 · 1 项待你验收");
    expect(progress(childrenOf(tree, "P1"), cat)).toEqual({ closed: 2, total: 5, cancelled: 1 });
    expect(line(list, "P4", ctx)).toBe("待办：整理文案");
    expect(progress(childrenOf(tree, "P4"), cat)).toMatchObject({ closed: 1, total: 2 });
    expect(line(list, "P5", ctx)).toBe("2 个进行中、1 个审核中 · 1 项待你验收");
    expect(progress(childrenOf(tree, "P5"), cat)).toMatchObject({ closed: 0, total: 3 });
    expect(line(list, "P6", ctx)).toBe("");
    expect(statusText(tree.byId.get("P6")!, ctx)).toBe("待你验收");
    expect(progress(childrenOf(tree, "P6"), cat)).toBeNull();
    expect(line(list, "P9", ctx)).toBe("阶段 14/16 · 1 个阻塞 · 1 项待你验收");
    expect(progress(childrenOf(tree, "P9"), cat)).toMatchObject({ closed: 13, total: 16 });
    expect(line(list, "P2", ctx)).toBe("");
  });

  it("an in-review issue the server has not listed yet is not counted and not 待你验收", () => {
    const list = dataset();
    const ctx = ctxOf(list, { awaiting_reply: [], in_review: [], in_review_by_others: [] });
    expect(line(list, "P1", ctx)).toBe("阶段 2/3 · 审核中：接口联调 · 1 个阻塞");
    const tree = buildTree(list);
    expect(statusText(tree.byId.get("P6")!, ctx)).toBe("审核中");
    expect(waitingOn(tree.byId.get("S3")!, ctx)).toBe("审核中");
  });

  it("a closed top-level issue is done whatever is below it; P2 waits once it waits for a reply", () => {
    const list = dataset().map((i) => (i.id === "P4" ? { ...i, status: "done" } : i));
    const att = sb4a(list, { awaiting_reply: [{ issue_id: "P2", ancestor_ids: [] }] });
    const g = leftGroups(buildTree(list), ctxOf(list, att));
    expect(g.done.map((i) => i.id)).toEqual(["P4"]);
    expect(g.waiting.map((i) => i.id)).toContain("P2");
  });
});

describe("A rule 7 sentences on Nest (N26: A6, A7, A9, A20–A23, A34, A37)", () => {
  const set = (list: Issue[], id: string, patch: Partial<Issue>) => list.map((i) => (i.id === id ? { ...i, ...patch } : i));

  it("A6, A7, A9", () => {
    let list = set(dataset(), "S4", { status: "done" });
    expect(line(list, "P1", ctxOf(list))).toBe("阶段 2/3 · 审核中：接口联调 · 1 项待你验收");
    list = set(list, "S3", { status: "done" });
    expect(line(list, "P1", ctxOf(list))).toBe("阶段 3/3 · 待规划：把导出报表页面的列宽和字体都调整成和设计…");
    expect(leftGroups(buildTree(list), ctxOf(list)).active.map((i) => i.id)).toContain("P1");
    list = set(list, "S5", { parent_issue_id: null });
    expect(line(list, "P1", ctxOf(list))).toBe("阶段已全部完成");
    expect(leftGroups(buildTree(list), ctxOf(list)).active.map((i) => i.id)).toContain("S5");
  });

  it("A20–A23", () => {
    const list = dataset();
    const a21 = sb4a(list, {
      awaiting_reply: [
        { issue_id: "R1", ancestor_ids: ["P5"] },
        { issue_id: "G2", ancestor_ids: ["C1", "P3"] },
        { issue_id: "P2", ancestor_ids: [] },
      ],
    });
    expect(line(list, "P5", ctxOf(list))).toBe("2 个进行中、1 个审核中 · 1 项待你验收");
    expect(line(list, "P5", ctxOf(list, a21))).toBe("2 个进行中、1 个审核中 · 1 项待你验收 · 1 项待你回复");
    expect(line(list, "P3", ctxOf(list, a21))).toBe("待办：C1 · 1 项待你回复");
    expect(line(list, "P2", ctxOf(list, a21))).toBe("1 项待你回复");
    expect(line(list, "P1", ctxOf(list, a21))).toBe("阶段 2/3 · 审核中：接口联调 · 1 个阻塞 · 1 项待你验收");
    const g = leftGroups(buildTree(list), ctxOf(list, a21));
    expect(g.waiting.map((i) => i.id)).toEqual(expect.arrayContaining(["P2", "P3"]));
    // N27: R1 itself waits for your reply.
    expect(waitingOn(buildTree(list).byId.get("R1")!, ctxOf(list, a21))).toBe("等你回复");
    // A22: you replied on R1; G2 cancelled.
    const a22list = set(list, "G2", { status: "cancelled" });
    const a22 = sb4a(a22list, { awaiting_reply: [{ issue_id: "P2", ancestor_ids: [] }] });
    expect(line(a22list, "P5", ctxOf(a22list, a22))).toBe("2 个进行中、1 个审核中 · 1 项待你验收");
    expect(line(a22list, "P3", ctxOf(a22list, a22))).toBe("待办：C1");
    expect(waitingOn(buildTree(a22list).byId.get("R1")!, ctxOf(a22list, a22))).toBe("没有负责人");
    expect(leftGroups(buildTree(a22list), ctxOf(a22list, a22)).active.map((i) => i.id)).toContain("P3");
  });

  it("A34 before rules 4–6", () => {
    const list = [...dataset(), issue("V", "backlog"), issue("V1", "in_review", { parent_issue_id: "V", assignee_type: "member", assignee_id: OTHER })];
    const ctx = ctxOf(list);
    expect(line(list, "V", ctx)).toBe("审核中：V1 · 1 项待你验收");
    expect(waitingOn(buildTree(list).byId.get("V1")!, ctx)).toBe("等你验收");
  });

  it("N34, N35 after rules 4–6: the reviewer comes from the server", () => {
    const list = [...dataset(), issue("V", "backlog", { assignee_type: "squad", assignee_id: "sq" }), issue("V1", "in_review", { parent_issue_id: "V" })];
    const att: Attention = {
      ...sb4a(list),
      in_review: sb4a(list).in_review.filter((i) => i.issue_id !== "V1"),
      in_review_by_others: [{ issue_id: "V1", reviewer: { type: "agent", id: "a-la" } }],
    };
    const ctx = ctxOf(list, att);
    expect(line(list, "V", ctx)).toBe("审核中：V1");
    expect(waitingOn(buildTree(list).byId.get("V1")!, ctx)).toBe("等 LA 验收");
    expect(statusText(buildTree(list).byId.get("V1")!, ctx)).toBe("审核中");
  });

  it("review requirement 3: a reviewer without a name reads 等其他人验收", () => {
    const list = [...dataset(), issue("V", "todo"), issue("V1", "in_review", { parent_issue_id: "V" })];
    const att: Attention = { ...sb4a(list), in_review: [], in_review_by_others: [{ issue_id: "V1", reviewer: { type: "member", id: "u-left" } }] };
    expect(waitingOn(buildTree(list).byId.get("V1")!, ctxOf(list, att))).toBe("等其他人验收");
  });

  it("A37: G3 in review below C1, then done", () => {
    const list = [...dataset(), issue("G3", "in_review", { parent_issue_id: "C1" })];
    expect(line(list, "P3", ctxOf(list))).toBe("待办：C1 · 1 项待你验收");
    expect(line(list, "P1", ctxOf(list))).toBe("阶段 2/3 · 审核中：接口联调 · 1 个阻塞 · 1 项待你验收");
    const tree = buildTree(list);
    expect(progress(childrenOf(tree, "C1"), cat)).toMatchObject({ closed: 1, total: 3 });
    const done = set(list, "G3", { status: "done" });
    expect(line(done, "P3", ctxOf(done))).toBe("待办：C1");
  });

  it("N29: Q2 done makes P4 子任务已全部完成", () => {
    const list = set(dataset(), "Q2", { status: "done" });
    expect(line(list, "P4", ctxOf(list))).toBe("子任务已全部完成");
    expect(progress(childrenOf(buildTree(list), "P4"), cat)).toMatchObject({ closed: 2, total: 2 });
  });
});

describe("middle column (N12, N13)", () => {
  it("N12: P1's header and sub-issue list", () => {
    const list = dataset();
    const ctx = ctxOf(list);
    const tree = buildTree(list);
    expect(waitingOn(tree.byId.get("P1")!, ctx)).toBe("没有负责人");
    const rows = stageRows(childrenOf(tree, "P1"), cat);
    expect(rows.map((r) => (r.kind === "folded" ? foldedLabel(r) : `阶段 ${r.stage}: ${r.issues.map((i) => i.id).join(",")}`))).toEqual([
      "阶段 1 · 已完成 2 个，含 1 个已取消",
      "阶段 2: S3,S4",
      "阶段 3: S5",
    ]);
    const by = (id: string) => tree.byId.get(id)!;
    expect(stripTitle(by("S3").title, "NEST-1")).toBe("接口联调");
    expect(waitingOn(by("S3"), ctx)).toBe("等你验收");
    expect(waitingOn(by("S4"), ctx)).toBe("阻塞，等你处理");
    expect(stripTitle(by("S5").title, "NEST-1")).toBe("把导出报表页面的列宽和字体都调整成和设计稿一致");
    expect(waitingOn(by("S5"), ctx)).toBe("待规划");
  });

  it("N13: P9 folds stages 1–13 into one line", () => {
    const list = dataset();
    const ctx = ctxOf(list);
    const tree = buildTree(list);
    const rows = stageRows(childrenOf(tree, "P9"), cat);
    const first = rows[0]!;
    expect(first.kind === "folded" && foldedLabel(first)).toBe("阶段 1–13 · 已完成 13 个");
    expect(first.kind === "folded" && first.groups.length).toBe(13);
    expect(rows.slice(1).map((r) => (r.kind === "group" ? waitingOn(r.issues[0]!, ctx) : null))).toEqual([
      "阻塞，等你处理",
      "等你验收",
      "等 other",
    ]);
  });

  it("a closed stage between open ones is not merged across", () => {
    const kids = [
      issue("a", "done", { stage: 1 }),
      issue("b", "todo", { stage: 2 }),
      issue("c", "done", { stage: 3 }),
      issue("d", "cancelled", { stage: 4 }),
    ];
    expect(stageRows(kids, cat).map((r) => (r.kind === "folded" ? foldedLabel(r) : `open ${r.stage}`))).toEqual([
      "阶段 1 · 已完成 1 个",
      "open 2",
      "阶段 3–4 · 已完成 2 个，含 1 个已取消",
    ]);
  });

  it("N15: tree levels list open children by stage, closed ones last", () => {
    const list = dataset();
    const tree = buildTree(list);
    const p1 = treeLevel(childrenOf(tree, "P1"), cat);
    expect(p1.open.map((i) => i.id)).toEqual(["S3", "S4", "S5"]);
    expect(p1.closed.length).toBe(2);
    const c1 = treeLevel(childrenOf(tree, "C1"), cat);
    expect(c1.open.map((i) => i.id)).toEqual(["G2"]);
    expect(c1.closed.length).toBe(1);
  });
});

describe("runs (review requirement 5)", () => {
  const task = (id: string, issueId: string, agent: string, status: string, started: string | null): SnapshotTask => ({
    id, issue_id: issueId, agent_id: agent, status, started_at: started, created_at: "2026-11-16T09:00:00Z",
  });

  it("dispatched, running and waiting_local_directory are running; queued is queued", () => {
    const runs = indexRuns([
      task("t1", "K16", "a-ma", "running", "2026-11-16T09:01:00Z"),
      task("t2", "K16", "a-la", "dispatched", "2026-11-16T09:00:30Z"),
      task("t3", "K16", "a-ma", "waiting_local_directory", null),
      task("t4", "K16", "a-ma", "queued", null),
      task("t5", "K16", "a-ma", "completed", "2026-11-16T08:00:00Z"),
    ]);
    const list = dataset();
    const ctx = ctxOf(list, sb4a(list), runs);
    expect(waitingOn(buildTree(list).byId.get("K16")!, ctx)).toBe("MA 等 3 个在做");
    expect(runCounts(runs, "K16", nm).sort()).toEqual(["LA 运行 1", "MA 运行 2 · 排队 1"]);
  });

  it("N33: one agent running two and queueing one", () => {
    const runs = indexRuns([
      task("t1", "X", "a-ma", "running", "2026-11-16T09:01:00Z"),
      task("t2", "X", "a-ma", "running", "2026-11-16T09:02:00Z"),
      task("t3", "X", "a-ma", "queued", null),
    ]);
    expect(runCounts(runs, "X", names([], [{ id: "a-ma", name: "TB" }], [], []))).toEqual(["TB 运行 2 · 排队 1"]);
  });
});
