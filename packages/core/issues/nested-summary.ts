import type { Issue } from "../types";
import { issueStatusCategory } from "./status-category";

export interface NestedChildGroup {
  /** null = the trailing "no stage" group. */
  stage: number | null;
  issues: Issue[];
}

export interface NestedSummary {
  total: number;
  /** done + cancelled — the same "closed" the server's stage gate uses. */
  closed: number;
  cancelled: number;
  /**
   * `{current, max}`: the lowest stage that still has an open sub-issue, and
   * the highest stage number. `"all_done"`: every staged sub-issue is closed.
   * null: no sub-issue carries a stage.
   */
  stage: { current: number; max: number } | "all_done" | null;
  inReview: number;
  blocked: number;
  /** Stage groups ascending, unstaged last; identifier number ascending. */
  groups: NestedChildGroup[];
}

/**
 * Card summary of a parent's DIRECT sub-issues (PRD A, rules 3–4). Closed and
 * stage follow the server's `stageProgress` (issue_wakeup_condition.go):
 * closed = status category done or closed, cancelled = category closed.
 * A custom status this response could not resolve counts as open.
 */
export function nestedSummary(children: readonly Issue[]): NestedSummary {
  let closed = 0;
  let cancelled = 0;
  let inReview = 0;
  let blocked = 0;
  const openStages = new Set<number>();
  const byStage = new Map<number | null, Issue[]>();
  for (const child of children) {
    const category = issueStatusCategory(child);
    const isClosed = category === "done" || category === "closed";
    if (isClosed) closed += 1;
    if (category === "closed") cancelled += 1;
    if (child.status === "in_review") inReview += 1;
    if (child.status === "blocked") blocked += 1;
    const stage = child.stage ?? null;
    if (stage !== null && !isClosed) openStages.add(stage);
    const bucket = byStage.get(stage);
    if (bucket) bucket.push(child);
    else byStage.set(stage, [child]);
  }
  const stages = [...byStage.keys()].filter((s): s is number => s !== null).sort((a, b) => a - b);
  const max = stages.at(-1);
  const stage =
    max === undefined
      ? null
      : openStages.size === 0
        ? ("all_done" as const)
        : { current: Math.min(...openStages), max };
  const groups: NestedChildGroup[] = [...stages, ...(byStage.has(null) ? [null] : [])].map(
    (s) => ({
      stage: s,
      issues: [...byStage.get(s)!].sort((a, b) => a.number - b.number),
    }),
  );
  return { total: children.length, closed, cancelled, stage, inReview, blocked, groups };
}
