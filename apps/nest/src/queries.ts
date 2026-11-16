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
  // An older server without the endpoint, or one before SB4a, degrades to
  // no counts rather than an error page.
  attention: (slug: string) =>
    queryOptions({ queryKey: keys.attention(slug), queryFn: () => api.getAttention(slug), retry: 1 }),
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
