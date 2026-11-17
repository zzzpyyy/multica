// TanStack Query keys and options. Everything a workspace shows sits under
// ["ws", slug], so a reconnect refetches it in one call (realtime.ts) and a
// workspace switch never mixes two workspaces' data.
import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import * as api from "./api";

export const keys = {
  me: ["me"] as const,
  workspaces: ["workspaces"] as const,
  ws: (slug: string) => ["ws", slug] as const,
  issues: (slug: string) => ["ws", slug, "issues"] as const,
  statuses: (slug: string) => ["ws", slug, "statuses"] as const,
  attention: (slug: string) => ["ws", slug, "attention"] as const,
  snapshot: (slug: string) => ["ws", slug, "snapshot"] as const,
  instancesAll: (slug: string) => ["ws", slug, "instances"] as const,
  instances: (slug: string, issueId: string) => ["ws", slug, "instances", issueId] as const,
  comments: (slug: string, issueId: string) => ["ws", slug, "comments", issueId] as const,
  members: (slug: string) => ["ws", slug, "members"] as const,
  agents: (slug: string) => ["ws", slug, "agents"] as const,
  squads: (slug: string) => ["ws", slug, "squads"] as const,
  projects: (slug: string) => ["ws", slug, "projects"] as const,
  superAgent: (slug: string) => ["ws", slug, "superAgent"] as const,
  runtimes: (slug: string) => ["ws", slug, "runtimes"] as const,
  messages: (slug: string, sessionId: string) => ["ws", slug, "messages", sessionId] as const,
  planCards: (slug: string, sessionId: string) => ["ws", slug, "planCards", sessionId] as const,
  pending: (slug: string, sessionId: string) => ["ws", slug, "pending", sessionId] as const,
};

export const q = {
  me: () => queryOptions({ queryKey: keys.me, queryFn: api.getMe, retry: false }),
  workspaces: () => queryOptions({ queryKey: keys.workspaces, queryFn: api.listWorkspaces }),
  issues: (slug: string) => queryOptions({ queryKey: keys.issues(slug), queryFn: () => api.listAllIssues(slug) }),
  statuses: (slug: string) => queryOptions({ queryKey: keys.statuses(slug), queryFn: () => api.listStatuses(slug) }),
  attention: (slug: string) => queryOptions({ queryKey: keys.attention(slug), queryFn: () => api.getAttention(slug) }),
  snapshot: (slug: string) => queryOptions({ queryKey: keys.snapshot(slug), queryFn: () => api.getSnapshot(slug) }),
  instances: (slug: string, issueId: string) =>
    queryOptions({ queryKey: keys.instances(slug, issueId), queryFn: () => api.listInstances(slug, issueId) }),
  comments: (slug: string, issueId: string) =>
    queryOptions({ queryKey: keys.comments(slug, issueId), queryFn: () => api.listComments(slug, issueId) }),
  members: (slug: string, workspaceId: string) =>
    queryOptions({ queryKey: keys.members(slug), queryFn: () => api.listMembers(slug, workspaceId) }),
  agents: (slug: string) => queryOptions({ queryKey: keys.agents(slug), queryFn: () => api.listAgents(slug) }),
  squads: (slug: string) => queryOptions({ queryKey: keys.squads(slug), queryFn: () => api.listSquads(slug) }),
  projects: (slug: string) => queryOptions({ queryKey: keys.projects(slug), queryFn: () => api.listProjects(slug) }),
  superAgent: (slug: string) =>
    queryOptions({ queryKey: keys.superAgent(slug), queryFn: () => api.ensureSuperAgent(slug), retry: false }),
  runtimes: (slug: string) => queryOptions({ queryKey: keys.runtimes(slug), queryFn: () => api.listRuntimes(slug) }),
  messages: (slug: string, sessionId: string) =>
    infiniteQueryOptions({
      queryKey: keys.messages(slug, sessionId),
      queryFn: ({ pageParam }) => api.messagesPage(slug, sessionId, pageParam),
      initialPageParam: null as api.MessagesCursor | null,
      getNextPageParam: (last) => (last.has_more ? last.next_cursor : null),
    }),
  planCards: (slug: string, sessionId: string) =>
    queryOptions({ queryKey: keys.planCards(slug, sessionId), queryFn: () => api.listPlanCards(slug, sessionId) }),
  pending: (slug: string, sessionId: string) =>
    queryOptions({ queryKey: keys.pending(slug, sessionId), queryFn: () => api.getPendingTask(slug, sessionId) }),
};

// ── When the issue columns may render (PRD v4 3.1.5, 一鸣 11-17) ───────────

interface Loadable {
  data: unknown;
  isError: boolean;
}

/** What the queries a workspace page's issue columns read look like now. */
export interface PageQueries {
  issues: Loadable;
  statuses: Loadable;
  attention: Loadable;
  snapshot: Loadable;
  /** Names for "等 X", "<智能体> 在做", "等 X 验收". */
  names: readonly Loadable[];
}

export type PageState =
  | { kind: "loading" }
  /** What is missing; the columns stay hidden rather than group without it. */
  | { kind: "failed"; what: string[] }
  | { kind: "ready" };

const settled = (r: Loadable) => r.data !== undefined || r.isError;
const missing = (r: Loadable) => r.data === undefined && r.isError;

/**
 * The left groups, the issue `/<slug>/issues` picks and every "在等谁" read
 * attention, the snapshot and the names. Before those arrive they would say
 * "nobody waits for you", and the picked issue is never picked again, so the
 * columns wait for them. A failed attention or snapshot is shown as a failure,
 * never as nobody waiting. Statuses and names degrade (built-in statuses,
 * generic names) as before.
 */
export function pageState(p: PageQueries): PageState {
  const what = [
    missing(p.issues) && "任务",
    missing(p.attention) && "等你处理的信息",
    missing(p.snapshot) && "智能体的运行情况",
  ].filter((x): x is string => !!x);
  if (what.length > 0) return { kind: "failed", what };
  const ready =
    p.issues.data !== undefined &&
    p.attention.data !== undefined &&
    p.snapshot.data !== undefined &&
    settled(p.statuses) &&
    p.names.every(settled);
  return ready ? { kind: "ready" } : { kind: "loading" };
}
