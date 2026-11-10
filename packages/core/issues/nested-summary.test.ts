// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Issue } from "../types";
import { nestedSummary } from "./nested-summary";

let seq = 0;
const child = (status: string, stage: number | null, extra: Partial<Issue> = {}): Issue =>
  ({ id: `c${++seq}`, number: seq, identifier: `T-${seq}`, status, stage, ...extra }) as Issue;

// PRD A test data P1–P4.
describe("nestedSummary", () => {
  it("P1: counts closed with cancelled, stage k/n, review and blocked (A2)", () => {
    const [s1, s2, s3, s4, s5] = [
      child("done", 1), child("cancelled", 1), child("in_review", 2), child("blocked", 2), child("backlog", 3),
    ];
    const s = nestedSummary([s5!, s3!, s1!, s4!, s2!]);
    expect(s).toMatchObject({ total: 5, closed: 2, cancelled: 1, inReview: 1, blocked: 1, stage: { current: 2, max: 3 } });
    // A4: stage groups ascending, number ascending inside a group.
    expect(s.groups.map((g) => [g.stage, g.issues.map((i) => i.id)])).toEqual([
      [1, [s1!.id, s2!.id]], [2, [s3!.id, s4!.id]], [3, [s5!.id]],
    ]);
  });

  it("A6/A7/A9: the open-stage frontier moves and finally reads all done", () => {
    const base = [child("done", 1), child("cancelled", 1), child("in_review", 2), child("done", 2)];
    expect(nestedSummary([...base, child("backlog", 3)]).stage).toEqual({ current: 2, max: 3 });
    base[2] = { ...base[2]!, status: "done" };
    expect(nestedSummary([...base, child("backlog", 3)])).toMatchObject({ closed: 4, inReview: 0, stage: { current: 3, max: 3 } });
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
