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
  blocked: number;
  /** Stage groups ascending, unstaged last; identifier number ascending. */
  groups: NestedChildGroup[];
}

function isClosed(issue: Issue): boolean {
  const category = issueStatusCategory(issue);
  return category === "done" || category === "closed";
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
  let blocked = 0;
  const openStages = new Set<number>();
  const byStage = new Map<number | null, Issue[]>();
  for (const child of children) {
    const closedChild = isClosed(child);
    if (closedChild) closed += 1;
    if (issueStatusCategory(child) === "closed") cancelled += 1;
    if (child.status === "blocked") blocked += 1;
    const stage = child.stage ?? null;
    if (stage !== null && !closedChild) openStages.add(stage);
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
  return { total: children.length, closed, cancelled, stage, blocked, groups };
}

// ─── One-line progress (PRD A rule 7, plan v2.3-A r3 §2.1) ────────────────

/** Titles in "当前进展" are capped at this many code points, then "…". */
export const PROGRESS_TITLE_MAX = 20;

/**
 * A sub-issue's title as the one-line progress shows it: the parent's
 * identifier and a leading "阶段 N：" are noise there.
 * 1. Drop the parent identifier and the spaces after it — unless a digit
 *    follows it, so BUNN-2 never eats into "BUNN-20".
 * 2. Drop a leading `阶段<n><colon>`; spaces optional, colon full- or
 *    half-width, spaces after it included.
 * 3. Nothing left: keep the original title.
 * 4. Longer than PROGRESS_TITLE_MAX code points: cut and append "…".
 */
export function progressTitle(title: string, parentIdentifier: string): string {
  let rest = title;
  if (
    parentIdentifier &&
    rest.startsWith(parentIdentifier) &&
    !/^\d/.test(rest.slice(parentIdentifier.length))
  ) {
    rest = rest.slice(parentIdentifier.length).trimStart();
  }
  rest = rest.replace(/^阶段\s*\d+\s*[:：]\s*/, "");
  if (rest === "") rest = title;
  const points = [...rest];
  return points.length > PROGRESS_TITLE_MAX
    ? `${points.slice(0, PROGRESS_TITLE_MAX).join("")}…`
    : rest;
}

/** One segment of the one-line progress, in display order. */
export type ProgressSegment =
  | { kind: "stage"; current: number; max: number }
  | { kind: "stage_all_done" }
  | { kind: "children_all_done" }
  /** Exactly one sub-issue in progress: its status and shortened title. */
  | { kind: "current"; status: string; title: string }
  /** Two or more: count per status, board column order, zeros left out. */
  | { kind: "current_counts"; counts: { status: string; n: number }[] }
  | { kind: "blocked"; n: number }
  | { kind: "awaiting_review"; n: number }
  | { kind: "awaiting_reply"; n: number };

export interface ProgressInput {
  parentIdentifier: string;
  /** Status keys in board column order — the status catalog's order. */
  statusOrder: readonly string[];
  /** "N 项待你验收" — from the attention endpoint, all levels below. */
  awaitingReview?: number;
  /** "m 项待你回复" — from the attention endpoint, this issue and below. */
  awaitingReply?: number;
}

/**
 * PRD A rule 7: the one-line progress of a parent, built from its DIRECT
 * sub-issues plus the two attention counts. Empty segments are left out; an
 * empty array means no line at all. Callers join the segments with " · ".
 */
export function progressSegments(
  children: readonly Issue[],
  { parentIdentifier, statusOrder, awaitingReview = 0, awaitingReply = 0 }: ProgressInput,
): ProgressSegment[] {
  const summary = nestedSummary(children);
  const segments: ProgressSegment[] = [];
  const { stage } = summary;
  if (stage === "all_done") segments.push({ kind: "stage_all_done" });
  else if (stage) segments.push({ kind: "stage", current: stage.current, max: stage.max });
  else if (summary.total > 0 && summary.closed === summary.total) {
    segments.push({ kind: "children_all_done" });
  }

  // Stage k's open, unblocked children; without a k, the unstaged ones.
  const k = stage && stage !== "all_done" ? stage.current : null;
  const moving = children.filter(
    (c) => (c.stage ?? null) === k && !isClosed(c) && c.status !== "blocked",
  );
  if (moving.length === 1) {
    const only = moving[0]!;
    segments.push({ kind: "current", status: only.status, title: progressTitle(only.title, parentIdentifier) });
  } else if (moving.length > 1) {
    const byStatus = new Map<string, number>();
    for (const c of moving) byStatus.set(c.status, (byStatus.get(c.status) ?? 0) + 1);
    const rank = (status: string) => {
      const i = statusOrder.indexOf(status);
      return i < 0 ? Number.MAX_SAFE_INTEGER : i;
    };
    const counts = [...byStatus]
      .map(([status, n]) => ({ status, n }))
      .sort((a, b) => rank(a.status) - rank(b.status) || a.status.localeCompare(b.status));
    segments.push({ kind: "current_counts", counts });
  }

  if (summary.blocked > 0) segments.push({ kind: "blocked", n: summary.blocked });
  if (awaitingReview > 0) segments.push({ kind: "awaiting_review", n: awaitingReview });
  if (awaitingReply > 0) segments.push({ kind: "awaiting_reply", n: awaitingReply });
  return segments;
}
