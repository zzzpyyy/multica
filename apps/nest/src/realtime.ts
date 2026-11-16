// /ws: one connection per workspace, reconnecting, and every event mapped to
// a cache update or an invalidation (plan section 4, review requirement 4).
// Task and comment events patch the cache from their payload; everything
// whose consequences the server decides (attention, snapshot, instances) is
// refetched.
import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import { normalizeComment, type ChatMessage, type Comment, type Issue, type MessagesPage } from "./api";
import { keys } from "./queries";

export interface EventContext {
  slug: string;
  /** The super-agent session, once known: chat events of other sessions are ignored. */
  sessionId: string | null;
  /** The viewer's user id: removing them from the workspace leaves it. */
  userId: string | null;
  /** Called when the viewer was removed from this workspace. */
  onRemovedFromWorkspace(): void;
}

interface WsEvent {
  type: string;
  payload?: Record<string, unknown> | null;
}

const TASK_EVENTS = new Set([
  "task:queued",
  "task:coalesced",
  "task:dispatch",
  "task:running",
  "task:waiting_local_directory",
  "task:completed",
  "task:failed",
  "task:cancelled",
]);

type CommentsData = { comments: Comment[]; truncated: boolean };

function mergeIssue(qc: QueryClient, slug: string, issue: Issue): void {
  qc.setQueryData<Issue[]>(keys.issues(slug), (old) => {
    if (!old) return old;
    const i = old.findIndex((x) => x.id === issue.id);
    if (i < 0) return [...old, issue];
    const cur = old[i]!;
    if (cur.revision !== undefined && issue.revision !== undefined && issue.revision < cur.revision) return old;
    const next = [...old];
    next[i] = { ...cur, ...issue };
    return next;
  });
}

function mergeComment(qc: QueryClient, slug: string, raw: Record<string, unknown>): void {
  if (typeof raw.id !== "string" || typeof raw.issue_id !== "string") return;
  const comment = normalizeComment(raw as Partial<Comment> & { id: string; issue_id: string });
  qc.setQueryData<CommentsData>(keys.comments(slug, comment.issue_id), (old) => {
    if (!old) return old;
    const i = old.comments.findIndex((c) => c.id === comment.id);
    const comments = [...old.comments];
    if (i < 0) comments.push(comment);
    else comments[i] = { ...comments[i]!, ...comment };
    return { ...old, comments };
  });
}

/** Writes a chat message into the newest page unless some page already has it. */
export function mergeMessage(qc: QueryClient, slug: string, sessionId: string, message: ChatMessage): void {
  qc.setQueryData<InfiniteData<MessagesPage>>(keys.messages(slug, sessionId), (old) => {
    if (!old || old.pages.length === 0) return old;
    if (old.pages.some((p) => p.messages.some((m) => m.id === message.id))) return old;
    const [first, ...rest] = old.pages;
    return { ...old, pages: [{ ...first!, messages: [...first!.messages, message] }, ...rest] };
  });
}

/** One event. Exported for tests: no socket involved. */
export function applyEvent(qc: QueryClient, ctx: EventContext, event: WsEvent): void {
  const { slug } = ctx;
  const p = event.payload ?? {};
  const invalidate = (key: readonly unknown[]) => void qc.invalidateQueries({ queryKey: key });
  const type = event.type;

  if (type === "issue:created" || type === "issue:updated") {
    if (p.issue && typeof (p.issue as Issue).id === "string") mergeIssue(qc, slug, p.issue as Issue);
    else invalidate(keys.issues(slug));
    // Any issue change can move "待你验收" or "待你回复": a small request,
    // not worth deciding field by field.
    invalidate(keys.attention(slug));
    return;
  }
  if (type === "issue:deleted") {
    invalidate(keys.issues(slug));
    invalidate(keys.attention(slug));
    return;
  }
  if (type === "comment:created" || type === "comment:updated") {
    // A deleted comment with replies arrives as comment:updated with deleted_at.
    if (p.comment && typeof p.comment === "object") mergeComment(qc, slug, p.comment as Record<string, unknown>);
    invalidate(keys.attention(slug));
    return;
  }
  if (type === "comment:deleted") {
    const id = p.comment_id;
    const issueId = p.issue_id;
    if (typeof id === "string" && typeof issueId === "string") {
      qc.setQueryData<CommentsData>(keys.comments(slug, issueId), (old) =>
        old ? { ...old, comments: old.comments.filter((c) => c.id !== id) } : old,
      );
    }
    invalidate(keys.attention(slug));
    return;
  }
  if (TASK_EVENTS.has(type)) {
    invalidate(keys.snapshot(slug));
    invalidate(keys.instancesAll(slug));
    if (ctx.sessionId && p.chat_session_id === ctx.sessionId) invalidate(keys.pending(slug, ctx.sessionId));
    return;
  }
  if (type === "daemon:register") {
    invalidate(keys.runtimes(slug));
    invalidate(keys.instancesAll(slug));
    return;
  }
  if (type === "chat:message" || type === "chat:done") {
    if (!ctx.sessionId || p.chat_session_id !== ctx.sessionId) return;
    if (typeof p.message_id === "string" && typeof p.content === "string" && p.content !== "") {
      mergeMessage(qc, slug, ctx.sessionId, {
        id: p.message_id,
        chat_session_id: ctx.sessionId,
        role: p.role === "user" ? "user" : "assistant",
        content: p.content,
        task_id: typeof p.task_id === "string" && p.task_id !== "" ? p.task_id : null,
        created_at: typeof p.created_at === "string" ? p.created_at : new Date().toISOString(),
      });
    }
    invalidate(keys.pending(slug, ctx.sessionId));
    if (type === "chat:done") invalidate(keys.planCards(slug, ctx.sessionId));
    return;
  }
  if (type.startsWith("squad:")) {
    // A new leader changes who reviews a squad's sub-issues.
    invalidate(keys.squads(slug));
    invalidate(keys.attention(slug));
    return;
  }
  if (type === "agent:created" || type === "agent:status") {
    invalidate(keys.agents(slug));
    return;
  }
  if (type === "agent:archived" || type === "agent:restored") {
    // An archived reviewer's sub-issues move to "待你验收" (review requirement 4).
    invalidate(keys.agents(slug));
    invalidate(keys.attention(slug));
    return;
  }
  if (type === "member:added" || type === "member:updated") {
    invalidate(keys.members(slug));
    return;
  }
  if (type === "member:removed") {
    invalidate(keys.members(slug));
    invalidate(keys.attention(slug));
    if (ctx.userId && p.user_id === ctx.userId) {
      invalidate(keys.workspaces);
      ctx.onRemovedFromWorkspace();
    }
    return;
  }
  if (type.startsWith("project:")) {
    invalidate(keys.projects(slug));
    return;
  }
  if (type === "issue_status:changed") {
    invalidate(keys.statuses(slug));
    return;
  }
  if (type === "workspace:updated" || type === "workspace:deleted") invalidate(keys.workspaces);
}

const DELAYS = [500, 1000, 2000];
const STEADY = 3000;

/**
 * Keeps one /ws connection open for `ctx.slug` until the returned function is
 * called. After every reconnect, and when the browser comes back online or to
 * the foreground, the workspace's queries are refetched: events sent while
 * the connection was down are gone.
 */
export function connect(qc: QueryClient, ctx: () => EventContext, slug: string): () => void {
  let socket: WebSocket | null = null;
  let attempt = 0;
  let opened = false;
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const refetchAll = () => void qc.invalidateQueries({ queryKey: keys.ws(slug) });

  const open = () => {
    clearTimeout(timer);
    if (stopped) return;
    const url = `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws?workspace_slug=${encodeURIComponent(slug)}&client_platform=nest`;
    const ws = new WebSocket(url);
    socket = ws;
    ws.onopen = () => {
      if (opened) refetchAll();
      opened = true;
      attempt = 0;
    };
    ws.onmessage = (e) => {
      try {
        applyEvent(qc, ctx(), JSON.parse(String(e.data)) as WsEvent);
      } catch {
        // a frame we cannot read: ignore it, the next refetch corrects
      }
    };
    ws.onclose = () => {
      if (socket !== ws || stopped) return;
      socket = null;
      // A close before ever opening counts as a reconnect too: the first
      // open after it must refetch whatever changed in between.
      opened = true;
      timer = setTimeout(open, DELAYS[attempt] ?? STEADY);
      attempt += 1;
    };
  };

  // A container cut off by `docker network disconnect` (N29) keeps a socket
  // that looks open and never delivers again. Coming back online or to the
  // foreground replaces it, then refetches.
  const restart = () => {
    if (stopped) return;
    const old = socket;
    socket = null;
    old?.close();
    attempt = 0;
    opened = true;
    open();
    refetchAll();
  };
  const onVisible = () => {
    if (document.visibilityState === "visible") restart();
  };
  window.addEventListener("online", restart);
  document.addEventListener("visibilitychange", onVisible);
  open();

  return () => {
    stopped = true;
    clearTimeout(timer);
    window.removeEventListener("online", restart);
    document.removeEventListener("visibilitychange", onVisible);
    socket?.close();
    socket = null;
  };
}
