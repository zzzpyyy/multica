import { QueryClient, type InfiniteData } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import type { Comment, Issue, MessagesPage } from "./api";
import { keys } from "./queries";
import { applyEvent, type EventContext } from "./realtime";

const SLUG = "nest-qa";
const SESSION = "sess-1";
const issue = (id: string, extra: Partial<Issue> = {}): Issue => ({
  id, number: 1, identifier: "NEST-1", title: id, description: null, status: "todo", assignee_type: null, assignee_id: null,
  parent_issue_id: null, project_id: null, stage: null, created_at: "2026-11-16T10:00:00Z", revision: 1, ...extra,
});

function setup() {
  const qc = new QueryClient();
  let removed = 0;
  const ctx: EventContext = { slug: SLUG, sessionId: SESSION, userId: "u-me", onRemovedFromWorkspace: () => (removed += 1) };
  const seed = (key: readonly unknown[], value: unknown) => qc.setQueryData(key, value);
  const stale = (key: readonly unknown[]) => qc.getQueryState(key)?.isInvalidated === true;
  seed(keys.attention(SLUG), { awaiting_reply: [], in_review: [], in_review_by_others: [] });
  seed(keys.issues(SLUG), [issue("a")]);
  seed(keys.comments(SLUG, "a"), { comments: [], truncated: false });
  for (const k of [keys.snapshot(SLUG), keys.instances(SLUG, "a"), keys.agents(SLUG), keys.members(SLUG), keys.squads(SLUG), keys.workspaces, keys.pending(SLUG, SESSION), keys.planCards(SLUG, SESSION), keys.statuses(SLUG), keys.projects(SLUG), keys.runtimes(SLUG)]) seed(k, []);
  seed(keys.messages(SLUG, SESSION), { pages: [{ messages: [], has_more: false, next_cursor: null }], pageParams: [null] });
  return { qc, ctx, stale, removed: () => removed, send: (type: string, payload: Record<string, unknown> = {}) => applyEvent(qc, ctx, { type, payload }) };
}

describe("applyEvent (plan 4.2)", () => {
  it("issue events merge by id, never back to an older revision, and refetch attention", () => {
    const t = setup();
    t.send("issue:updated", { issue: issue("a", { status: "done", revision: 3 }) });
    t.send("issue:updated", { issue: issue("a", { status: "todo", revision: 2 }) });
    t.send("issue:created", { issue: issue("b") });
    expect(t.qc.getQueryData<Issue[]>(keys.issues(SLUG))!.map((i) => [i.id, i.status])).toEqual([["a", "done"], ["b", "todo"]]);
    expect(t.stale(keys.attention(SLUG))).toBe(true);
  });

  it("issue:deleted refetches issues and attention", () => {
    const t = setup();
    t.send("issue:deleted", { issue_id: "a" });
    expect(t.stale(keys.issues(SLUG))).toBe(true);
    expect(t.stale(keys.attention(SLUG))).toBe(true);
  });

  it("a service-written comment with 9 fields is filled in; a tombstone replaces; a delete removes", () => {
    const t = setup();
    t.send("comment:created", {
      comment: { id: "c1", issue_id: "a", author_type: "agent", author_id: "x", content: "hi", type: "comment", parent_id: null, source_task_id: "t", created_at: "2026-11-16T10:00:00Z" },
    });
    let data = t.qc.getQueryData<{ comments: Comment[] }>(keys.comments(SLUG, "a"))!;
    expect(data.comments[0]).toMatchObject({ id: "c1", attachments: [], deleted_at: null, updated_at: "2026-11-16T10:00:00Z" });
    t.send("comment:updated", { comment: { ...data.comments[0], deleted_at: "2026-11-16T11:00:00Z" } });
    data = t.qc.getQueryData<{ comments: Comment[] }>(keys.comments(SLUG, "a"))!;
    expect(data.comments[0]!.deleted_at).toBe("2026-11-16T11:00:00Z");
    t.send("comment:deleted", { comment_id: "c1", issue_id: "a" });
    expect(t.qc.getQueryData<{ comments: Comment[] }>(keys.comments(SLUG, "a"))!.comments).toEqual([]);
    expect(t.stale(keys.attention(SLUG))).toBe(true);
  });

  it.each(["agent:archived", "agent:restored", "member:removed", "squad:updated"])("%s refetches attention (review requirement 4)", (type) => {
    const t = setup();
    t.send(type, { user_id: "u-other" });
    expect(t.stale(keys.attention(SLUG))).toBe(true);
  });

  it("agent:status refetches names only", () => {
    const t = setup();
    t.send("agent:status", {});
    expect(t.stale(keys.agents(SLUG))).toBe(true);
    expect(t.stale(keys.attention(SLUG))).toBe(false);
  });

  it("being removed yourself leaves the workspace", () => {
    const t = setup();
    t.send("member:removed", { user_id: "u-me" });
    expect(t.removed()).toBe(1);
    expect(t.stale(keys.workspaces)).toBe(true);
  });

  it.each(["task:dispatch", "task:waiting_local_directory", "task:completed"])("%s refetches runs and instances", (type) => {
    const t = setup();
    t.send(type, { chat_session_id: SESSION });
    expect(t.stale(keys.snapshot(SLUG))).toBe(true);
    expect(t.stale(keys.instances(SLUG, "a"))).toBe(true);
    expect(t.stale(keys.pending(SLUG, SESSION))).toBe(true);
  });

  it("chat messages of the super-agent session are written once; others are ignored", () => {
    const t = setup();
    const msg = { chat_session_id: SESSION, message_id: "m1", role: "assistant", content: "push", created_at: "2026-11-16T10:00:00Z" };
    t.send("chat:message", msg);
    t.send("chat:message", msg);
    t.send("chat:message", { ...msg, chat_session_id: "other", message_id: "m2" });
    const pages = t.qc.getQueryData<InfiniteData<MessagesPage>>(keys.messages(SLUG, SESSION))!.pages;
    expect(pages[0]!.messages.map((m) => [m.id, m.task_id])).toEqual([["m1", null]]);
    t.send("chat:done", { chat_session_id: SESSION, task_id: "t1" });
    expect(t.stale(keys.planCards(SLUG, SESSION))).toBe(true);
  });
});
