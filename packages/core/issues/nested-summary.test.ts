// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue } from "../types";
import { nestedSummary, progressSegments, progressTitle } from "./nested-summary";

let seq = 0;
const child = (status: string, stage: number | null, extra: Partial<Issue> = {}): Issue =>
  ({ id: `c${++seq}`, number: seq, identifier: `T-${seq}`, status, stage, ...extra }) as Issue;

// PRD A test data P1–P4.
describe("nestedSummary", () => {
  it("P1: counts closed with cancelled, stage k/n and blocked (A2)", () => {
    const [s1, s2, s3, s4, s5] = [
      child("done", 1), child("cancelled", 1), child("in_review", 2), child("blocked", 2), child("backlog", 3),
    ];
    const s = nestedSummary([s5!, s3!, s1!, s4!, s2!]);
    expect(s).toMatchObject({ total: 5, closed: 2, cancelled: 1, blocked: 1, stage: { current: 2, max: 3 } });
    // A4: stage groups ascending, number ascending inside a group.
    expect(s.groups.map((g) => [g.stage, g.issues.map((i) => i.id)])).toEqual([
      [1, [s1!.id, s2!.id]], [2, [s3!.id, s4!.id]], [3, [s5!.id]],
    ]);
  });

  it("A6/A7/A9: the open-stage frontier moves and finally reads all done", () => {
    const base = [child("done", 1), child("cancelled", 1), child("in_review", 2), child("done", 2)];
    expect(nestedSummary([...base, child("backlog", 3)]).stage).toEqual({ current: 2, max: 3 });
    base[2] = { ...base[2]!, status: "done" };
    expect(nestedSummary([...base, child("backlog", 3)])).toMatchObject({ closed: 4, stage: { current: 3, max: 3 } });
    // S5 detached: 4/4 and every stage closed.
    expect(nestedSummary(base)).toMatchObject({ total: 4, closed: 4, stage: "all_done" });
  });

  it("P2/P4: no children, or no staged children, has no stage (A3)", () => {
    expect(nestedSummary([])).toMatchObject({ total: 0, closed: 0, stage: null, groups: [] });
    const p4 = nestedSummary([child("todo", null), child("done", null)]);
    expect(p4).toMatchObject({ total: 2, closed: 1, stage: null });
    expect(p4.groups.map((g) => g.stage)).toEqual([null]);
  });

  it("puts unstaged children in a trailing group", () => {
    const s = nestedSummary([child("todo", null), child("todo", 2), child("todo", 1)]);
    expect(s.groups.map((g) => g.stage)).toEqual([1, 2, null]);
  });

  it("reads custom statuses by their server category", () => {
    const s = nestedSummary([
      child("shipped", 1, { status_category: "done" }),
      child("dropped", 1, { status_category: "closed" }),
      child("mystery", 2),
    ]);
    expect(s).toMatchObject({ closed: 2, cancelled: 1, stage: { current: 2, max: 2 } });
  });
});

// Plan v2.3-A r3 §2.1: the six title examples, verbatim.
describe("progressTitle", () => {
  it.each([
    ["阶段2：接口联调", "BUNN-101", "接口联调"],
    ["BUNN-101 阶段 3: 把导出报表页面的列宽和字体都调整成和设计稿一致", "BUNN-101", "把导出报表页面的列宽和字体都调整成和设计…"],
    ["BUNN-20 阶段 15：发消息时把页面选中的 Tiki 空间带给 Agent", "BUNN-20", "发消息时把页面选中的 Tiki 空间带给…"],
    ["阶段3：服务端开发（按阶段2契约）", "BUNN-26", "服务端开发（按阶段2契约）"],
    ["BUNN-20 修复", "BUNN-2", "BUNN-20 修复"],
    ["阶段 4：", "BUNN-9", "阶段 4："],
  ])("%s (parent %s) → %s", (title, parent, shown) => {
    expect(progressTitle(title, parent)).toBe(shown);
  });

  it("counts code points, not UTF-16 units", () => {
    expect(progressTitle("😀".repeat(20), "X-1")).toBe("😀".repeat(20));
    expect(progressTitle("😀".repeat(21), "X-1")).toBe(`${"😀".repeat(20)}…`);
  });
});

// PRD v3.4 A rule 7 with the test data P1–P5, V and A37's G3. The rendered
// sentences are asserted verbatim in views (nested-board/card-summary.test.tsx).
describe("progressSegments", () => {
  const order = ["backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled"];
  const P1 = "BUNN-101";
  const titled = (title: string, status: string, stage: number | null) => child(status, stage, { title });
  const p1 = () => [
    titled("S1", "done", 1),
    titled("S2", "cancelled", 1),
    titled("阶段2：接口联调", "in_review", 2),
    titled("S4", "blocked", 2),
    titled(`${P1} 阶段 3: 把导出报表页面的列宽和字体都调整成和设计稿一致`, "backlog", 3),
  ];
  const opts = { parentIdentifier: P1, statusOrder: order };

  it("A2: stage, the one moving child, blocked, awaiting review", () => {
    expect(progressSegments(p1(), { ...opts, awaitingReview: 1 })).toEqual([
      { kind: "stage", current: 2, max: 3 },
      { kind: "current", status: "in_review", title: "接口联调" },
      { kind: "blocked", n: 1 },
      { kind: "awaiting_review", n: 1 },
    ]);
  });

  it("A6, A7, A9: the frontier moves to stage 3, then all stages done", () => {
    const kids = p1();
    kids[3] = { ...kids[3]!, status: "done" };
    expect(progressSegments(kids, { ...opts, awaitingReview: 1 })).toEqual([
      { kind: "stage", current: 2, max: 3 },
      { kind: "current", status: "in_review", title: "接口联调" },
      { kind: "awaiting_review", n: 1 },
    ]);
    kids[2] = { ...kids[2]!, status: "done" };
    expect(progressSegments(kids, opts)).toEqual([
      { kind: "stage", current: 3, max: 3 },
      { kind: "current", status: "backlog", title: "把导出报表页面的列宽和字体都调整成和设计…" },
    ]);
    expect(progressSegments(kids.slice(0, 4), opts)).toEqual([{ kind: "stage_all_done" }]);
  });

  it("A3: no children and no attention is no line; unstaged children read as one", () => {
    expect(progressSegments([], opts)).toEqual([]);
    expect(progressSegments([titled("Q1", "done", null), titled("整理文案", "todo", null)], { ...opts, parentIdentifier: "BUNN-104" }))
      .toEqual([{ kind: "current", status: "todo", title: "整理文案" }]);
  });

  it("A20/A21: several unstaged children count by status in column order", () => {
    const p5 = [titled("R3", "in_review", null), titled("R1", "in_progress", null), titled("R2", "in_progress", null)];
    expect(progressSegments(p5, { ...opts, awaitingReview: 1, awaitingReply: 1 })).toEqual([
      { kind: "current_counts", counts: [{ status: "in_progress", n: 2 }, { status: "in_review", n: 1 }] },
      { kind: "awaiting_review", n: 1 },
      { kind: "awaiting_reply", n: 1 },
    ]);
  });

  it("A21: a parent without children still shows its replies", () => {
    expect(progressSegments([], { ...opts, awaitingReply: 1 })).toEqual([{ kind: "awaiting_reply", n: 1 }]);
  });

  it("A34 before rules 4–6, A37: review counts come from below, not from the children", () => {
    expect(progressSegments([titled("V1", "in_review", null)], { ...opts, awaitingReview: 1 })).toEqual([
      { kind: "current", status: "in_review", title: "V1" },
      { kind: "awaiting_review", n: 1 },
    ]);
    // P3: C1 is todo; its child G3 in review only shows through the count.
    expect(progressSegments([titled("C1", "todo", null)], { ...opts, awaitingReview: 1 })).toEqual([
      { kind: "current", status: "todo", title: "C1" },
      { kind: "awaiting_review", n: 1 },
    ]);
  });

  it("all unstaged children closed reads 子任务已全部完成", () => {
    expect(progressSegments([titled("a", "done", null), titled("b", "cancelled", null)], opts)).toEqual([
      { kind: "children_all_done" },
    ]);
  });

  it("an unknown status sorts after the catalog's", () => {
    const kids = [titled("a", "shipping", null), titled("b", "todo", null)];
    expect(progressSegments(kids, opts)[0]).toEqual({
      kind: "current_counts",
      counts: [{ status: "todo", n: 1 }, { status: "shipping", n: 1 }],
    });
  });
});
