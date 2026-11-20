// Pure rules of the task page (plan 6.1). Written for Nest from PRD A rule 7
// (v3.4), plan v2.3-A r3 section 2.1 and PRD v4 sections 3.1–3.3; nothing is
// copied from the old pages.
//
// Ceiling: Nest reads every issue of the workspace once (api.ts
// listAllIssues) and builds the tree here. Fine for the ~60 issues of today;
// past ~2000 switch to open issues in one read plus closed ones per top-level.
import type { Agent, Attention, InstanceBatch, Issue, Member, SnapshotTask, Squad, StatusEntry } from "./api";

// ── Statuses ────────────────────────────────────────────────────────────

/** Built-in statuses: the server leaves their names empty, Nest names them. */
const BUILT_IN: Record<string, string> = {
  backlog: "待规划",
  todo: "待办",
  in_progress: "进行中",
  in_review: "审核中",
  done: "已完成",
  blocked: "已阻塞",
  cancelled: "已取消",
};
const BUILT_IN_ORDER = Object.keys(BUILT_IN);

export interface Catalog {
  /** Status keys in board column order. */
  order: string[];
  label(status: string): string;
  /** Category key: backlog, todo, in_progress, in_review, blocked, done, cancelled. */
  category(issue: Pick<Issue, "status" | "status_category">): string;
}

export function catalog(entries: readonly StatusEntry[]): Catalog {
  const sorted = [...entries].sort((a, b) => a.position - b.position);
  const byKey = new Map(sorted.map((e) => [e.key, e]));
  return {
    order: sorted.length > 0 ? sorted.map((e) => e.key) : BUILT_IN_ORDER,
    label: (status) => BUILT_IN[status] ?? byKey.get(status)?.name ?? status,
    category: (issue) => byKey.get(issue.status)?.category ?? issue.status_category ?? issue.status,
  };
}

/** Closed is done or cancelled, the same reading the server's stage gate uses. */
export const isClosed = (issue: Issue, cat: Catalog) => {
  const c = cat.category(issue);
  return c === "done" || c === "cancelled" || c === "closed";
};
const isCancelled = (issue: Issue, cat: Catalog) => {
  const c = cat.category(issue);
  return c === "cancelled" || c === "closed";
};

// ── Time ────────────────────────────────────────────────────────────────

/**
 * RFC 3339 times by value, never as strings: the API writes "+08:00" and
 * chat:done writes "Z", both valid (BUNN-31 问题 3). Milliseconds only; ties
 * fall back to the caller's next key. Unreadable times count as 0.
 */
export function timeOf(iso: string): number {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? 0 : t;
}
export const compareTime = (a: string, b: string) => timeOf(a) - timeOf(b);

/** A send whose answer got lost: when it left, and what was already on screen then. */
export interface Attempt {
  /** Milliseconds; a few seconds early, for the browser's clock running ahead. */
  since: number;
  known: ReadonlySet<string>;
}

/**
 * The first try of a resend that landed after all: mine, same text, written
 * after it left, not on screen before it, and not what another send on this
 * page already ended with (`claimed`, BUNN-31 Fail 2).
 */
export function landed<T extends { id: string; created_at: string; content: string }>(
  items: readonly T[],
  text: string,
  attempt: Attempt,
  mine: (item: T) => boolean,
  claimed: ReadonlySet<string>,
): T | undefined {
  return items.find(
    (x) =>
      mine(x) && !claimed.has(x.id) && x.content.trim() === text.trim() && !attempt.known.has(x.id) && timeOf(x.created_at) >= attempt.since,
  );
}

/**
 * Chat sends (BUNN-31 Fail 2). A retry first looks for its earlier try among
 * the newest messages; each send ends with its own server message, and no
 * two sends end with the same one. Side by side, a retry took the same-text
 * message another send was still waiting on, and its own text was never
 * sent. Sends of the same text go one after another, so a retry looks only
 * once the send before it knows its message; other texts never match, so
 * they don't wait.
 */
// ponytail: a hung send holds the next send of the same text; give sendMessage a timeout if that shows up.
export function sendQueue<T extends { id: string; created_at: string; content: string }>(
  newest: () => Promise<readonly T[]>,
  post: (text: string) => Promise<T>,
  mine: (item: T) => boolean,
) {
  const claimed = new Set<string>();
  const tails = new Map<string, Promise<unknown>>();
  return (text: string, attempt: Attempt, retry: boolean): Promise<{ item: T; earlier: boolean }> => {
    const key = text.trim();
    const run = (tails.get(key) ?? Promise.resolve()).then(async () => {
      const found = retry ? landed(await newest(), text, attempt, mine, claimed) : undefined;
      const item = found ?? (await post(text));
      claimed.add(item.id);
      return { item, earlier: !!found };
    });
    const tail = run.catch(() => undefined);
    tails.set(key, tail);
    void tail.then(() => tails.get(key) === tail && tails.delete(key));
    return run;
  };
}

// ── Tree ────────────────────────────────────────────────────────────────

export interface Tree {
  byId: Map<string, Issue>;
  /** Direct children per parent id. */
  children: Map<string, Issue[]>;
  /** Issues whose parent is not in the list: the top level (PRD A rule 2). */
  roots: Issue[];
}

const MAX_DEPTH = 32;

export function buildTree(issues: readonly Issue[]): Tree {
  const byId = new Map(issues.map((i) => [i.id, i]));
  const children = new Map<string, Issue[]>();
  const roots: Issue[] = [];
  for (const issue of issues) {
    const parent = issue.parent_issue_id;
    if (parent && byId.has(parent) && parent !== issue.id) {
      const list = children.get(parent);
      if (list) list.push(issue);
      else children.set(parent, [issue]);
    } else {
      roots.push(issue);
    }
  }
  return { byId, children, roots };
}

export const childrenOf = (tree: Tree, id: string): Issue[] => tree.children.get(id) ?? [];

/** Ancestors from the top-level issue down to the parent. Stops after 32 levels (a cycle in bad data). */
export function ancestors(tree: Tree, issue: Issue): Issue[] {
  const chain: Issue[] = [];
  let parent = issue.parent_issue_id ? tree.byId.get(issue.parent_issue_id) : undefined;
  while (parent && chain.length < MAX_DEPTH && parent.id !== issue.id) {
    chain.unshift(parent);
    parent = parent.parent_issue_id ? tree.byId.get(parent.parent_issue_id) : undefined;
  }
  return chain;
}

export function topLevelOf(tree: Tree, issue: Issue): Issue {
  return ancestors(tree, issue)[0] ?? issue;
}

function hasBlockedBelowOrSelf(tree: Tree, issue: Issue, depth = 0): boolean {
  if (issue.status === "blocked") return true;
  if (depth >= MAX_DEPTH) return false;
  return childrenOf(tree, issue.id).some((c) => hasBlockedBelowOrSelf(tree, c, depth + 1));
}

// ── Titles ──────────────────────────────────────────────────────────────

const STAGE_PREFIX = /^阶段\s*\d+\s*[:：]\s*/;

/**
 * A sub-issue's title without the noise (plan v2.3-A r3 §2.1):
 * 1. the parent identifier and the spaces after it, unless a digit follows
 *    (a BUNN-2 child must keep "BUNN-20");
 * 2. a leading `阶段<n><colon>`, spaces optional, either colon width;
 * 3. nothing left: the original title;
 * 4. `max` code points kept, then "…" (left column and one-line progress use
 *    20; the sub-issue list does not cut).
 */
export function stripTitle(title: string, parentIdentifier: string | null, max?: number): string {
  let out = title;
  if (parentIdentifier && out.startsWith(parentIdentifier)) {
    const after = out.slice(parentIdentifier.length);
    if (!/^\d/.test(after)) out = after.replace(/^\s+/, "");
  }
  out = out.replace(STAGE_PREFIX, "");
  if (out.length === 0) out = title;
  if (max === undefined) return out;
  const points = Array.from(out);
  return points.length > max ? `${points.slice(0, max).join("")}…` : out;
}

// ── Attention ───────────────────────────────────────────────────────────

export interface AttentionIndex {
  /** In-review issues the server says you review. */
  mine: Set<string>;
  /** In-review issues someone else reviews, with the reviewer. */
  others: Map<string, { type: "member" | "agent"; id: string }>;
  /** Issues waiting for your reply. */
  replyOwn: Set<string>;
  /** "N 项待你验收": per issue, your in-review issues strictly below it. */
  reviewBelow: Map<string, number>;
  /** "m 项待你回复": per issue, waiting replies on it and below it. */
  replyAtOrBelow: Map<string, number>;
}

export function indexAttention(a: Attention): AttentionIndex {
  const bump = (m: Map<string, number>, id: string) => m.set(id, (m.get(id) ?? 0) + 1);
  const reviewBelow = new Map<string, number>();
  const replyAtOrBelow = new Map<string, number>();
  for (const item of a.in_review) new Set(item.ancestor_ids).forEach((id) => bump(reviewBelow, id));
  for (const item of a.awaiting_reply) new Set([item.issue_id, ...item.ancestor_ids]).forEach((id) => bump(replyAtOrBelow, id));
  return {
    mine: new Set(a.in_review.map((i) => i.issue_id)),
    others: new Map(a.in_review_by_others.map((i) => [i.issue_id, i.reviewer])),
    replyOwn: new Set(a.awaiting_reply.map((i) => i.issue_id)),
    reviewBelow,
    replyAtOrBelow,
  };
}

export const EMPTY_ATTENTION: AttentionIndex = indexAttention({ awaiting_reply: [], in_review: [], in_review_by_others: [] });

// ── One-line progress (A rule 7) ────────────────────────────────────────

export interface Part {
  text: string;
  /** "待你验收" and "待你回复" stand out. */
  callToAction: boolean;
  /** "N 个阻塞" stands out too, in the blocked color. */
  alarm?: boolean;
}

export const SEPARATOR = " · ";

/**
 * Up to five parts: stage, current progress, blocked, awaiting your review,
 * awaiting your reply. The first three read the DIRECT children only; the
 * last two come from `attention`. No parts means no line.
 */
export function oneLine(issue: Issue, kids: readonly Issue[], att: AttentionIndex, cat: Catalog): Part[] {
  const parts: Part[] = [];
  const plain = (text: string) => parts.push({ text, callToAction: false });

  const staged = kids.filter((k) => k.stage !== null);
  const openStages = staged.filter((k) => !isClosed(k, cat)).map((k) => k.stage!);
  let frontier: number | null = null;
  if (staged.length > 0) {
    const last = Math.max(...staged.map((k) => k.stage!));
    if (openStages.length === 0) plain("阶段已全部完成");
    else {
      frontier = Math.min(...openStages);
      plain(`阶段 ${frontier}/${last}`);
    }
  } else if (kids.length > 0 && kids.every((k) => isClosed(k, cat))) {
    plain("子任务已全部完成");
  }

  const moving = kids.filter((k) => k.stage === frontier && !isClosed(k, cat) && k.status !== "blocked");
  if (moving.length === 1) {
    const only = moving[0]!;
    plain(`${cat.label(only.status)}：${stripTitle(only.title, issue.identifier, 20)}`);
  } else if (moving.length > 1) {
    const tally = new Map<string, number>();
    for (const k of moving) tally.set(k.status, (tally.get(k.status) ?? 0) + 1);
    // Board column order; a status the catalog does not know goes last.
    const rank = (s: string) => {
      const i = cat.order.indexOf(s);
      return i < 0 ? cat.order.length : i;
    };
    plain(
      [...tally.entries()]
        .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
        .map(([status, n]) => `${n} 个${cat.label(status)}`)
        .join("、"),
    );
  }

  const blocked = kids.filter((k) => k.status === "blocked").length;
  if (blocked > 0) parts.push({ text: `${blocked} 个阻塞`, callToAction: false, alarm: true });
  const review = att.reviewBelow.get(issue.id) ?? 0;
  if (review > 0) parts.push({ text: `${review} 项待你验收`, callToAction: true });
  const reply = att.replyAtOrBelow.get(issue.id) ?? 0;
  if (reply > 0) parts.push({ text: `${reply} 项待你回复`, callToAction: true });
  return parts;
}

export const lineText = (parts: readonly Part[]) => parts.map((p) => p.text).join(SEPARATOR);

/** `closed/total` and "含 N 个已取消" over the direct children; null without children. */
export function progress(kids: readonly Issue[], cat: Catalog): { closed: number; total: number; cancelled: number } | null {
  if (kids.length === 0) return null;
  return {
    closed: kids.filter((k) => isClosed(k, cat)).length,
    total: kids.length,
    cancelled: kids.filter((k) => isCancelled(k, cat)).length,
  };
}

// ── Names ───────────────────────────────────────────────────────────────

export interface Names {
  member(userId: string): string | null;
  agent(id: string): string | null;
  squad(id: string): string | null;
  project(id: string): string | null;
  /** The super-agent's id, to mark its comments. */
  superAgentId: string | null;
}

export function names(members: readonly Member[], agents: readonly Agent[], squads: readonly Squad[], projects: readonly { id: string; title: string }[]): Names {
  const m = new Map(members.map((x) => [x.user_id, x.name || x.email]));
  const a = new Map(agents.map((x) => [x.id, x.name]));
  const s = new Map(squads.map((x) => [x.id, x.name]));
  const p = new Map(projects.map((x) => [x.id, x.title]));
  return {
    member: (id) => m.get(id) ?? null,
    agent: (id) => a.get(id) ?? null,
    squad: (id) => s.get(id) ?? null,
    project: (id) => p.get(id) ?? null,
    superAgentId: agents.find((x) => x.system_key === "super")?.id ?? null,
  };
}

export function actorName(n: Names, type: string | null, id: string | null): string | null {
  if (!type || !id) return null;
  if (type === "member") return n.member(id);
  if (type === "agent") return n.agent(id);
  if (type === "squad") return n.squad(id);
  return null;
}

// ── Runs ────────────────────────────────────────────────────────────────

/** Running, per the v2.2 InstanceStatus reading (review requirement 5). */
export const RUNNING = new Set(["dispatched", "running", "waiting_local_directory"]);

export interface RunIndex {
  /** Runs in progress per issue, earliest start first. */
  running: Map<string, SnapshotTask[]>;
  /** Per issue: "<agent> 运行 m · 排队 q" counts, per agent. */
  counts: Map<string, { agentId: string; running: number; queued: number }[]>;
}

export function indexRuns(snapshot: readonly SnapshotTask[]): RunIndex {
  const running = new Map<string, SnapshotTask[]>();
  const tallies = new Map<string, Map<string, { agentId: string; running: number; queued: number }>>();
  for (const t of snapshot) {
    if (!t.issue_id) continue;
    const isRunning = RUNNING.has(t.status);
    if (!isRunning && t.status !== "queued") continue;
    if (isRunning) {
      const list = running.get(t.issue_id);
      if (list) list.push(t);
      else running.set(t.issue_id, [t]);
    }
    const perAgent = tallies.get(t.issue_id) ?? new Map();
    const row = perAgent.get(t.agent_id) ?? { agentId: t.agent_id, running: 0, queued: 0 };
    if (isRunning) row.running += 1;
    else row.queued += 1;
    perAgent.set(t.agent_id, row);
    tallies.set(t.issue_id, perAgent);
  }
  const startOf = (t: SnapshotTask) => t.started_at ?? t.created_at;
  for (const list of running.values()) list.sort((a, b) => compareTime(startOf(a), startOf(b)));
  return { running, counts: new Map([...tallies].map(([id, m]) => [id, [...m.values()]])) };
}

export const EMPTY_RUNS: RunIndex = { running: new Map(), counts: new Map() };

export function runCounts(runs: RunIndex, issueId: string, n: Names): string[] {
  return (runs.counts.get(issueId) ?? []).map((c) => {
    const who = n.agent(c.agentId) ?? "智能体";
    return c.queued > 0 ? `${who} 运行 ${c.running} · 排队 ${c.queued}` : `${who} 运行 ${c.running}`;
  });
}

/**
 * Which instance wrote a comment (D rule 7): its source_task_id is one of the
 * instance's attempts. Same reading as the old page's findTaskInstance.
 */
export function commentInstance(
  batches: readonly InstanceBatch[],
  sourceTaskId: string | null | undefined,
): { index: number; total: number; split: string } | null {
  if (!sourceTaskId) return null;
  for (const b of batches) {
    const i = b.instances.find((x) => x.task_id === sourceTaskId || (x.task_ids ?? []).includes(sourceTaskId));
    if (i) return { index: i.index, total: b.total, split: i.split };
  }
  return null;
}

// ── Waiting on ──────────────────────────────────────────────────────────

export interface Ctx {
  cat: Catalog;
  att: AttentionIndex;
  runs: RunIndex;
  names: Names;
}

/** "In waiting for": the first row of PRD v4 3.2.3 that holds; null for closed issues. */
export function waitingOn(issue: Issue, ctx: Ctx): string | null {
  const { cat, att, runs, names: n } = ctx;
  if (att.replyOwn.has(issue.id)) return "等你回复";
  if (issue.status === "in_review") {
    if (att.mine.has(issue.id)) return "等你验收";
    const reviewer = att.others.get(issue.id);
    if (reviewer) {
      const who = reviewer.type === "member" ? n.member(reviewer.id) : n.agent(reviewer.id);
      return who ? `等 ${who} 验收` : "等其他人验收";
    }
    return "审核中";
  }
  if (issue.status === "blocked") return "阻塞，等你处理";
  const active = runs.running.get(issue.id) ?? [];
  if (active.length > 0) {
    const first = n.agent(active[0]!.agent_id) ?? "智能体";
    return active.length === 1 ? `${first} 在做` : `${first} 等 ${active.length} 个在做`;
  }
  const c = cat.category(issue);
  if (c === "done" || c === "cancelled" || c === "closed") return null;
  if (c === "backlog") return "待规划";
  if (!issue.assignee_type || !issue.assignee_id) return "没有负责人";
  const who = actorName(n, issue.assignee_type, issue.assignee_id);
  return who ? `等 ${who}` : "等其他人";
}

/** The status in a cell's tooltip: "待你验收" when the server says you review it. */
export const statusText = (issue: Issue, ctx: Ctx) =>
  issue.status === "in_review" && ctx.att.mine.has(issue.id) ? "待你验收" : ctx.cat.label(issue.status);

/**
 * The status written on a row and in the detail header (PRD v4 3.1.3, 3.2.2):
 * the plain label, as the detail does since BUNN-26 ("审核中", with "等你验收"
 * beside it). Left out when 在等谁 already says it: "待规划" twice, "已阻塞"
 * next to "阻塞，等你处理", "审核中" with no reviewer known.
 */
export function shownStatus(issue: Issue, waiting: string | null, cat: Catalog): string | null {
  const label = cat.label(issue.status);
  if (waiting === label) return null;
  if (issue.status === "blocked" && waiting?.startsWith("阻塞")) return null;
  return label;
}

// ── Left column (PRD v4 3.1) ────────────────────────────────────────────

export interface Groups {
  waiting: Issue[];
  active: Issue[];
  done: Issue[];
}

const newestFirst = (a: Issue, b: Issue) => compareTime(b.created_at, a.created_at) || b.number - a.number;

/**
 * Three fixed groups, newest first inside each. A closed top-level issue is
 * in "已完成" whatever is below it. Waiting means: you review it; something
 * below waits for your review or reply, or it waits for your reply itself;
 * or it or anything below is blocked.
 */
export function leftGroups(tree: Tree, ctx: Ctx): Groups {
  const groups: Groups = { waiting: [], active: [], done: [] };
  for (const root of tree.roots) {
    if (isClosed(root, ctx.cat)) groups.done.push(root);
    else if (
      ctx.att.mine.has(root.id) ||
      (ctx.att.reviewBelow.get(root.id) ?? 0) > 0 ||
      (ctx.att.replyAtOrBelow.get(root.id) ?? 0) > 0 ||
      hasBlockedBelowOrSelf(tree, root)
    )
      groups.waiting.push(root);
    else groups.active.push(root);
  }
  groups.waiting.sort(newestFirst);
  groups.active.sort(newestFirst);
  groups.done.sort(newestFirst);
  return groups;
}

/** The issue `/<slug>/issues` opens: first waiting, else first active. */
export const defaultIssue = (g: Groups): Issue | null => g.waiting[0] ?? g.active[0] ?? null;

// ── Sub-issue list (PRD v4 3.2.2) ───────────────────────────────────────

export type StageRow =
  | { kind: "group"; stage: number | null; issues: Issue[] }
  /** Adjacent fully closed stages folded into one line. */
  | { kind: "folded"; from: number | null; to: number | null; closed: number; cancelled: number; groups: { stage: number | null; issues: Issue[] }[] };

export function stageRows(kids: readonly Issue[], cat: Catalog): StageRow[] {
  const byStage = new Map<number | null, Issue[]>();
  for (const k of kids) {
    const list = byStage.get(k.stage);
    if (list) list.push(k);
    else byStage.set(k.stage, [k]);
  }
  const stages = [...byStage.keys()].filter((s): s is number => s !== null).sort((a, b) => a - b);
  const ordered: (number | null)[] = byStage.has(null) ? [...stages, null] : stages;
  const rows: StageRow[] = [];
  for (const stage of ordered) {
    const issues = [...byStage.get(stage)!].sort((a, b) => a.number - b.number);
    const allClosed = issues.every((i) => isClosed(i, cat));
    const prev = rows[rows.length - 1];
    if (!allClosed) {
      rows.push({ kind: "group", stage, issues });
    } else if (prev?.kind === "folded" && stage !== null && prev.to !== null) {
      prev.to = stage;
      prev.closed += issues.length;
      prev.cancelled += issues.filter((i) => isCancelled(i, cat)).length;
      prev.groups.push({ stage, issues });
    } else {
      rows.push({
        kind: "folded",
        from: stage,
        to: stage,
        closed: issues.length,
        cancelled: issues.filter((i) => isCancelled(i, cat)).length,
        groups: [{ stage, issues }],
      });
    }
  }
  return rows;
}

export function foldedLabel(row: Extract<StageRow, { kind: "folded" }>): string {
  const where = row.from === null ? "未分阶段" : row.from === row.to ? `阶段 ${row.from}` : `阶段 ${row.from}–${row.to}`;
  return `${where} · 已完成 ${row.closed} 个${row.cancelled > 0 ? `，含 ${row.cancelled} 个已取消` : ""}`;
}

/** Stage order (unstaged last), then number: the segmented bar's cells. */
export const byStage = (a: Issue, b: Issue) =>
  (a.stage ?? Number.MAX_SAFE_INTEGER) - (b.stage ?? Number.MAX_SAFE_INTEGER) || a.number - b.number;

// ── Filters (the tree's search and "只看要我处理的") ──────────────────────

/** Waits on you: your review, your reply, or blocked (the "等你处理" reasons). */
export const needsYou = (issue: Issue, att: AttentionIndex) =>
  (issue.status === "in_review" && att.mine.has(issue.id)) || att.replyOwn.has(issue.id) || issue.status === "blocked";

/** The color an issue gets in the tree: red blocked, orange waiting on you, gray otherwise. */
export function tone(issue: Issue, att: AttentionIndex, cat: Catalog): "bad" | "cta" | "closed" | "" {
  if (att.replyOwn.has(issue.id)) return "cta";
  if (issue.status === "blocked") return "bad";
  if (needsYou(issue, att)) return "cta";
  return isClosed(issue, cat) ? "closed" : "";
}

/** The issues `keep` holds for, and every ancestor of them, so each match is shown in place. */
export function visibleIssues(tree: Tree, keep: (issue: Issue) => boolean): Set<string> {
  const out = new Set<string>();
  for (const issue of tree.byId.values()) {
    if (!keep(issue) || out.has(issue.id)) continue;
    out.add(issue.id);
    for (const a of ancestors(tree, issue)) out.add(a.id);
  }
  return out;
}
