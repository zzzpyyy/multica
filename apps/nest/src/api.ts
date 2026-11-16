// HTTP calls to the fork server, all same-origin through server.mjs (plan
// section 2.3). Only the fields Nest reads are typed. No token is stored:
// the login cookie authenticates every request, including /ws.

export type ActorType = "member" | "agent" | "squad";

export interface Me {
  id: string;
  name: string;
  email: string;
}

export interface Workspace {
  id: string;
  name: string;
  slug: string;
}

export interface Issue {
  id: string;
  number: number;
  identifier: string;
  title: string;
  description: string | null;
  status: string;
  /** Server-resolved category key, e.g. "done", "cancelled". */
  status_category?: string;
  /** A custom status's name; empty for the seven built-ins. */
  status_name?: string;
  assignee_type: ActorType | null;
  assignee_id: string | null;
  parent_issue_id: string | null;
  project_id: string | null;
  stage: number | null;
  created_at: string;
  revision?: number;
}

export interface StatusEntry {
  key: string;
  name: string;
  category: string;
  position: number;
  is_system?: boolean;
  archived_at?: string | null;
}

export interface AttentionItem {
  issue_id: string;
  /** Parent chain, nearest first, up to the top-level issue. */
  ancestor_ids: string[];
}

export interface ReviewByOthersItem {
  issue_id: string;
  reviewer: { type: "member" | "agent"; id: string };
}

/** `GET /api/issues/attention`, BUNN-28 group-2 contract (SB4a and later). */
export interface Attention {
  awaiting_reply: AttentionItem[];
  /** In-review issues you review, top-level ones included. */
  in_review: AttentionItem[];
  /** In-review sub-issues someone else reviews. */
  in_review_by_others: ReviewByOthersItem[];
}

export interface SnapshotTask {
  id: string;
  issue_id: string | null;
  agent_id: string;
  status: string;
  started_at: string | null;
  created_at: string;
  chat_session_id?: string | null;
}

export interface Attachment {
  id: string;
  filename: string;
  content_type?: string;
  /** Same-origin, cookie-authenticated; serves media inline. */
  download_url: string;
}

export interface Comment {
  id: string;
  issue_id: string;
  author_type: string;
  author_id: string;
  content: string;
  type: string;
  parent_id: string | null;
  created_at: string;
  updated_at: string;
  deleted_at: string | null;
  attachments: Attachment[];
}

export interface Member {
  user_id: string;
  name: string;
  email: string;
}

export interface Agent {
  id: string;
  name: string;
  system_key?: string | null;
  archived_at?: string | null;
}

export interface Squad {
  id: string;
  name: string;
  leader_id: string;
}

export interface Project {
  id: string;
  title: string;
}

export interface Runtime {
  id: string;
  name: string;
  provider: string;
  status: string;
}

/** A message of the super-agent session. `task_id` is null on a push. */
export interface ChatMessage {
  id: string;
  created_at: string;
  role: "user" | "assistant";
  content: string;
  chat_session_id: string;
  task_id: string | null;
}

export interface MessagesCursor {
  created_at: string;
  id: string;
}

export interface MessagesPage {
  messages: ChatMessage[];
  has_more: boolean;
  next_cursor: MessagesCursor | null;
}

export interface PlanCardIssueItem {
  title: string;
  assignee: { type: ActorType; id: string; name: string };
  what: string;
  done: string;
  status: string;
  will_run: boolean;
}

export interface PlanCard {
  id: string;
  chat_message_id: string;
  kind: "create_issues" | "add_instances";
  status: "pending" | "creating" | "created" | "failed" | "cancelled" | "expired";
  error: string | null;
  items?: PlanCardIssueItem[];
  created_issues?: { id: string; identifier: string }[];
  issue?: { id: string; identifier: string; title: string };
  agent?: { id: string; name: string };
  splits?: string[];
  priority?: boolean;
}

/** A follow-up sent while the super-agent is still busy. */
export interface QueuedChatTask {
  task_id: string;
  message_id?: string;
  content?: string;
  created_at: string;
}

export interface PendingTask {
  task_id?: string;
  status?: string;
  /**
   * Follow-ups waiting behind the running turn. The messages page leaves
   * these out until their turn starts, so they are shown from here (B rule 9).
   */
  queued_tasks?: QueuedChatTask[];
}

export interface InstanceQueue {
  position: number;
  reason: string;
  running: number | null;
  limit: number | null;
}

export interface Instance {
  index: number;
  split: string;
  status: string;
  queue: InstanceQueue | null;
}

export interface InstanceBatch {
  id: string;
  agent: { id: string; name: string };
  total: number;
  instances: Instance[];
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string | null,
    message: string,
  ) {
    super(message);
  }
  /** The request never reached the server, or the proxy could not reach it. */
  get offline(): boolean {
    return this.status === 0 || this.status === 502 || this.status === 503 || this.status === 504;
  }
}

let onUnauthorized: () => void = () => {};
/** Called on any 401: the app clears its cache and shows the login page. */
export function setUnauthorizedHandler(fn: () => void): void {
  onUnauthorized = fn;
}

function cookie(name: string): string | null {
  for (const part of document.cookie.split(";")) {
    const [k, ...v] = part.trim().split("=");
    if (k === name) return decodeURIComponent(v.join("="));
  }
  return null;
}

/** The two CSRF cookies the server accepts, preferred first. */
function csrfTokens(): string[] {
  return [cookie("multica_csrf_session"), cookie("multica_csrf")].filter((t): t is string => !!t);
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  /** Workspace slug, sent as X-Workspace-Slug. */
  slug?: string;
}

async function send(path: string, opts: RequestOptions, csrf: string | null): Promise<Response> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if (opts.slug) headers["X-Workspace-Slug"] = opts.slug;
  if (csrf) headers["X-CSRF-Token"] = csrf;
  try {
    return await fetch(path, {
      method: opts.method ?? "GET",
      headers,
      credentials: "same-origin",
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
    });
  } catch {
    throw new ApiError(0, null, "network error");
  }
}

async function errorOf(res: Response): Promise<ApiError> {
  let code: string | null = null;
  let message = res.statusText;
  try {
    const body = (await res.json()) as { code?: string; error?: string };
    code = body.code ?? null;
    message = body.error ?? message;
  } catch {
    // not JSON
  }
  return new ApiError(res.status, code, message);
}

/** One call. Mutating methods carry X-CSRF-Token; a 403 retries once with the other cookie. */
export async function request<T>(path: string, opts: RequestOptions = {}): Promise<{ data: T; headers: Headers }> {
  const mutating = (opts.method ?? "GET") !== "GET";
  const tokens = mutating ? csrfTokens() : [];
  let res = await send(path, opts, tokens[0] ?? null);
  // The server accepts either cookie; after a session renewal an open tab can
  // hold a stale one. A 403 means nothing ran, so retrying is safe.
  if (res.status === 403 && tokens.length > 1 && tokens[1] !== tokens[0]) res = await send(path, opts, tokens[1]!);
  if (res.status === 401) {
    onUnauthorized();
    throw await errorOf(res);
  }
  if (!res.ok) throw await errorOf(res);
  const text = await res.text();
  return { data: (text ? JSON.parse(text) : null) as T, headers: res.headers };
}

const get = async <T>(path: string, slug?: string): Promise<T> => (await request<T>(path, { slug })).data;

// ── Auth and workspaces ─────────────────────────────────────────────────

export const sendCode = (email: string) => request("/auth/send-code", { method: "POST", body: { email } });
export const verifyCode = (email: string, code: string) =>
  request("/auth/verify-code", { method: "POST", body: { email, code } });
export const logout = () => request("/auth/logout", { method: "POST" });
export const getMe = () => get<Me>("/api/me");
export const listWorkspaces = () => get<Workspace[]>("/api/workspaces");

// ── Issues ──────────────────────────────────────────────────────────────

const PAGE = 100;

/**
 * Every issue in the workspace, oldest first, deduplicated by id. Nest builds
 * the tree itself (plan 3.2): the top level is "parent not in this list".
 *
 * Paged by created_at ascending (the server breaks ties by id), so a new issue
 * lands at the end. A deletion mid-way shifts later rows forward and can skip
 * one, with no event to bring it back, so a count that disagrees with the last
 * page's total refetches the whole list once.
 *
 * Ceiling: one full read per load. Past ~2000 issues, fetch open issues with
 * `open_only=true` in one go and page closed ones by top-level issue.
 */
export async function listAllIssues(slug: string): Promise<Issue[]> {
  const readAll = async () => {
    const byId = new Map<string, Issue>();
    let total = 0;
    for (let offset = 0; ; offset += PAGE) {
      const page = await get<{ issues: Issue[]; total: number }>(
        `/api/issues?sort=created_at&direction=asc&limit=${PAGE}&offset=${offset}`,
        slug,
      );
      for (const issue of page.issues) byId.set(issue.id, issue);
      total = page.total;
      if (page.issues.length < PAGE || offset + PAGE >= total) break;
    }
    return { issues: [...byId.values()], total };
  };
  const first = await readAll();
  if (first.issues.length === first.total) return first.issues;
  return (await readAll()).issues;
}

export async function listStatuses(slug: string): Promise<StatusEntry[]> {
  return (await get<{ statuses: StatusEntry[] }>("/api/issue-statuses", slug)).statuses ?? [];
}

const items = (v: unknown): AttentionItem[] =>
  Array.isArray(v)
    ? v.filter((x): x is AttentionItem => !!x && typeof x.issue_id === "string").map((x) => ({
        issue_id: x.issue_id,
        ancestor_ids: Array.isArray(x.ancestor_ids) ? x.ancestor_ids.filter((a: unknown) => typeof a === "string") : [],
      }))
    : [];

/** Malformed data degrades to empty lists rather than breaking the page. */
export function normalizeAttention(raw: unknown): Attention {
  const r = (raw ?? {}) as Record<string, unknown>;
  const others = Array.isArray(r.in_review_by_others) ? r.in_review_by_others : [];
  return {
    awaiting_reply: items(r.awaiting_reply),
    in_review: items(r.in_review),
    in_review_by_others: others.filter(
      (x): x is ReviewByOthersItem =>
        !!x && typeof x.issue_id === "string" && !!x.reviewer && typeof x.reviewer.id === "string" &&
        (x.reviewer.type === "member" || x.reviewer.type === "agent"),
    ),
  };
}

export const getAttention = async (slug: string) => normalizeAttention(await get<unknown>("/api/issues/attention", slug));
export const getSnapshot = async (slug: string) => (await get<SnapshotTask[] | null>("/api/agent-task-snapshot", slug)) ?? [];
export const listInstances = async (slug: string, issueId: string) =>
  (await get<{ batches: InstanceBatch[] }>(`/api/issues/${issueId}/instances`, slug)).batches ?? [];

/** Mark done. A stale revision gets 409 instead of overwriting a newer status. */
export const approve = (slug: string, issue: Issue) =>
  request<Issue>(`/api/issues/${issue.id}`, {
    method: "PUT",
    slug,
    body: issue.revision === undefined ? { status: "done" } : { status: "done", expected_revision: issue.revision },
  });

// ── Comments ────────────────────────────────────────────────────────────

/** Fills fields a service-written comment event leaves out (plan 4.2). */
export function normalizeComment(raw: Partial<Comment> & { id: string; issue_id: string }): Comment {
  return {
    author_type: "member",
    author_id: "",
    content: "",
    type: "comment",
    parent_id: null,
    created_at: raw.created_at ?? new Date().toISOString(),
    ...raw,
    updated_at: raw.updated_at ?? raw.created_at ?? new Date().toISOString(),
    deleted_at: raw.deleted_at ?? null,
    attachments: raw.attachments ?? [],
  };
}

/**
 * Every comment of an issue in one read (plan 3.2): threads are ordered by
 * their root's creation time, which the server's `recent` paging does not
 * keep. Ceiling: the server returns at most 2000 and sets
 * X-Comments-Truncated; past that, page with `recent`.
 */
export async function listComments(slug: string, issueId: string): Promise<{ comments: Comment[]; truncated: boolean }> {
  const { data, headers } = await request<Comment[]>(`/api/issues/${issueId}/comments`, { slug });
  return {
    comments: (data ?? []).map(normalizeComment),
    truncated: headers.get("X-Comments-Truncated") === "true",
  };
}

export const createComment = async (slug: string, issueId: string, content: string, parentId: string | null) =>
  normalizeComment(
    (await request<Comment>(`/api/issues/${issueId}/comments`, {
      method: "POST",
      slug,
      body: parentId ? { content, parent_id: parentId } : { content },
    })).data,
  );

// ── Names ───────────────────────────────────────────────────────────────

export const listMembers = (slug: string, workspaceId: string) =>
  get<Member[]>(`/api/workspaces/${workspaceId}/members`, slug);
/** Archived agents too: they still own issues and wrote comments. */
export const listAgents = async (slug: string) => (await get<Agent[] | null>("/api/agents?include_archived=true", slug)) ?? [];
export const listSquads = async (slug: string) => (await get<Squad[] | null>("/api/squads", slug)) ?? [];
export const listProjects = async (slug: string) =>
  (await get<{ projects: Project[] }>("/api/projects", slug)).projects ?? [];

// ── super-agent chat ────────────────────────────────────────────────────

/** Idempotent. 409 `runtime_required` until the workspace has set it up. */
export async function ensureSuperAgent(slug: string, runtimeId?: string): Promise<{ agentId: string; sessionId: string }> {
  const { data } = await request<{ id: string; session: { id: string } }>("/api/agents/super-agent", {
    method: "POST",
    slug,
    body: runtimeId ? { runtime_id: runtimeId } : undefined,
  });
  return { agentId: data.id, sessionId: data.session.id };
}

export const listRuntimes = async (slug: string) => (await get<Runtime[] | null>("/api/runtimes", slug)) ?? [];

export async function messagesPage(slug: string, sessionId: string, before: MessagesCursor | null): Promise<MessagesPage> {
  const q = new URLSearchParams({ limit: "50" });
  if (before) {
    q.set("before_created_at", before.created_at);
    q.set("before_id", before.id);
  }
  const page = await get<MessagesPage>(`/api/chat/sessions/${sessionId}/messages/page?${q}`, slug);
  return { messages: page.messages ?? [], has_more: !!page.has_more, next_cursor: page.next_cursor ?? null };
}

export const sendMessage = async (slug: string, sessionId: string, content: string) =>
  (await request<{ message_id: string }>(`/api/chat/sessions/${sessionId}/messages`, {
    method: "POST",
    slug,
    body: { content },
  })).data;

export const listPlanCards = async (slug: string, sessionId: string) =>
  (await get<{ cards: PlanCard[] }>(`/api/chat/sessions/${sessionId}/plan-cards`, slug)).cards ?? [];
export const decidePlanCard = async (slug: string, id: string, decision: "confirm" | "cancel") =>
  (await request<PlanCard>(`/api/chat/plan-cards/${id}/${decision}`, { method: "POST", slug })).data;
export const getPendingTask = (slug: string, sessionId: string) =>
  get<PendingTask>(`/api/chat/sessions/${sessionId}/pending-task`, slug);
