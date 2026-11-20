import { describe, expect, it } from "vitest";
import type { ChatMessage, Comment, Issue } from "./api";
import { buildTree, catalog, compareTime, EMPTY_ATTENTION, EMPTY_RUNS, landed, leftGroups, names } from "./model";
import { threads } from "./ui/Comments";

// BUNN-31 问题 3 (一鸣 11-20): the API writes "+08:00", chat:done writes "Z".
// Both are RFC 3339; compared as strings, a reply that came in live sorted
// above the day's later messages, and a resend took an 8-hour-old message
// with the same text for its own first try.
const LOCAL = (hms: string) => `2026-11-20T${hms}+08:00`;
const UTC = (hms: string) => `2026-11-20T${hms}Z`;

describe("times by value", () => {
  it("sorts the two spellings together", () => {
    // 12:13:58.5Z is 20:13:58.5 local: after 20:10:25 local, before 20:14:00 local.
    const times = [LOCAL("20:14:00"), UTC("12:13:58.512345678"), LOCAL("20:10:25.123456"), UTC("11:00:00")];
    expect([...times].sort(compareTime)).toEqual([UTC("11:00:00"), LOCAL("20:10:25.123456"), UTC("12:13:58.512345678"), LOCAL("20:14:00")]);
    // As strings it was the other way round: "2026-11-20T12…Z" < "2026-11-20T20…+08:00".
    expect(UTC("12:13:58") < LOCAL("20:10:25")).toBe(true);
    expect(compareTime(UTC("12:13:58"), LOCAL("20:10:25"))).toBeGreaterThan(0);
  });

  it("comment threads and the tree's newest-first order read the value", () => {
    const c = (id: string, created_at: string): Comment =>
      ({ id, issue_id: "i", author_type: "member", author_id: "u", content: id, parent_id: null, created_at, attachments: [] }) as unknown as Comment;
    expect(threads([c("late", UTC("12:30:00")), c("early", LOCAL("20:10:00"))]).map((t) => t.root.id)).toEqual(["early", "late"]);

    const i = (id: string, created_at: string, number: number): Issue => ({
      id, number, identifier: id, title: id, description: null, status: "todo", assignee_type: null, assignee_id: null,
      parent_issue_id: null, project_id: null, stage: null, created_at,
    });
    const tree = buildTree([i("older", LOCAL("20:10:00"), 1), i("newer", UTC("12:30:00"), 2)]);
    const g = leftGroups(tree, { cat: catalog([]), att: EMPTY_ATTENTION, runs: EMPTY_RUNS, names: names([], [], [], []) });
    expect(g.active.map((x) => x.id)).toEqual(["newer", "older"]);
  });
});

describe("a resend recognizes its first try (Chat.tsx, Comments.tsx)", () => {
  // The first try left at 12:00:00Z (browser clock), 5 s slack.
  const attempt = (known: string[] = []) => ({ since: Date.parse(UTC("11:59:55")), known: new Set(known) });

  it("chat: the same text 3 hours earlier, written +08:00, is not this try", () => {
    const m = (id: string, created_at: string, role: ChatMessage["role"] = "user"): ChatMessage =>
      ({ id, chat_session_id: "s", role, content: "你好", task_id: null, created_at });
    const user = (x: ChatMessage) => x.role === "user";
    // "2026-11-20T17:00:00+08:00" >= "2026-11-20T11:59:55.000Z" as strings: the old check took it.
    expect(landed([m("old", LOCAL("17:00:00"))], "你好 ", attempt(), user, new Set())).toBeUndefined();
    // The try that did land, in either spelling.
    expect(landed([m("old", LOCAL("17:00:00")), m("new", LOCAL("20:00:01"))], "你好", attempt(), user, new Set())?.id).toBe("new");
    expect(landed([m("new", UTC("12:00:01"))], "你好", attempt(), user, new Set())?.id).toBe("new");
    // Not the super-agent's message, not one that was on screen before the try.
    expect(landed([m("reply", LOCAL("20:00:02"), "assistant")], "你好", attempt(), user, new Set())).toBeUndefined();
    expect(landed([m("shown", LOCAL("20:00:01"))], "你好", attempt(["shown"]), user, new Set())).toBeUndefined();
  });

  it("comments: the same text in the same box 3 hours earlier is not this try", () => {
    const c = (id: string, created_at: string, extra: Partial<Comment> = {}) =>
      ({ id, author_id: "u-me", parent_id: null, content: "收到", created_at, ...extra }) as Comment;
    const mine = (x: Comment) => x.author_id === "u-me" && x.parent_id === null;
    expect(landed([c("old", LOCAL("17:00:00"))], "收到", attempt(), mine, new Set())).toBeUndefined();
    expect(landed([c("old", LOCAL("17:00:00")), c("new", LOCAL("20:00:01"))], "收到", attempt(), mine, new Set())?.id).toBe("new");
    expect(landed([c("other", LOCAL("20:00:01"), { author_id: "u-other" })], "收到", attempt(), mine, new Set())).toBeUndefined();
    expect(landed([c("reply", LOCAL("20:00:01"), { parent_id: "root" })], "收到", attempt(), mine, new Set())).toBeUndefined();
  });
});
