import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { ChatMessage } from "./api";
import { landed, sendQueue } from "./model";
import { atBottomAfterScroll, followGrowth } from "./ui/Chat";

// BUNN-31: the two Chat.tsx fails in 马云's 14:32Z retest, fixed per 一鸣.

describe("at the bottom, a message that grows keeps the list at the bottom (Fail 1)", () => {
  class FakeObserver {
    static last: FakeObserver;
    observed: unknown[] = [];
    constructor(readonly fire: () => void) {
      FakeObserver.last = this;
    }
    observe(el: unknown) {
      this.observed.push(el);
    }
    unobserve() {}
    disconnect() {
      this.observed = [];
    }
  }

  it("follows a plan card loading after its reply, and leaves a scrolled-up reader alone", () => {
    // 600 px tall, at the bottom: scrollTop + 600 = scrollHeight.
    const list = { scrollTop: 400, scrollHeight: 1000 };
    let pinned = true;
    const body = {} as Element;
    const stop = followGrowth(list, body, () => pinned, FakeObserver as unknown as typeof ResizeObserver);
    expect(FakeObserver.last.observed).toEqual([body]);

    // The card comes in: 155 px more, and the number of messages is the same.
    list.scrollHeight = 1155;
    FakeObserver.last.fire();
    expect(list.scrollTop).toBe(1155);

    // Scrolled up: the list does not jump.
    pinned = false;
    list.scrollTop = 200;
    list.scrollHeight = 1300;
    FakeObserver.last.fire();
    expect(list.scrollTop).toBe(200);

    stop();
    expect(FakeObserver.last.observed).toEqual([]);
  });

  it("the scroll event comes with the grown height before the observer: still at the bottom (马云 15:33Z)", () => {
    // 马云's log: the reply comes in, the page scrolls to the bottom (scrollTop
    // 4403, 600 px tall); in the same frame the card adds 214 px; the scroll
    // event of that scroll fires first and reads the taller list, then the
    // ResizeObserver callback runs.
    const list = { scrollTop: 4000, scrollHeight: 4600, clientHeight: 600 };
    let pinned = true;
    let lastTop = list.scrollTop;
    const onScroll = () => {
      pinned = atBottomAfterScroll(pinned, list, lastTop);
      lastTop = list.scrollTop;
    };
    followGrowth(list, {} as Element, () => pinned, FakeObserver as unknown as typeof ResizeObserver);
    list.scrollHeight = 5003;
    list.scrollTop = 4403; // the page's own scroll to the bottom
    list.scrollHeight = 5217; // the card, same frame
    onScroll();
    FakeObserver.last.fire();
    expect(pinned).toBe(true);
    expect(list.scrollTop).toBe(5217);

    // The reader scrolls up 700 px: off the bottom, a growing card leaves it there.
    list.scrollTop = 5217 - 600 - 700;
    onScroll();
    expect(pinned).toBe(false);
    list.scrollHeight = 5400;
    FakeObserver.last.fire();
    expect(list.scrollTop).toBe(3917);

    // Scrolling down short of the bottom keeps it off; reaching it puts it back.
    list.scrollTop = 4200;
    onScroll();
    expect(pinned).toBe(false);
    list.scrollTop = 4790;
    onScroll();
    expect(pinned).toBe(true);

    // "正在回复…" going away clamps scrollTop down to the bottom: still at the bottom.
    list.scrollHeight = 5360;
    list.scrollTop = 4760;
    onScroll();
    expect(pinned).toBe(true);
  });

  it("the conversation hooks it up on the list and the wrapper round its messages", () => {
    // Nest's tests have no DOM to run the effect in; the browser check covers it running.
    const src = readFileSync(new URL("./ui/Chat.tsx", import.meta.url), "utf8");
    expect(src).toMatch(/return followGrowth\(list\.current, body\.current, \(\) => atBottom\.current\);/);
    expect(src).toMatch(/atBottom\.current = atBottomAfterScroll\(atBottom\.current, el, lastTop\.current\);\s*lastTop\.current = el\.scrollTop;/);
    expect(src).toMatch(/ref=\{list\} onScroll=\{onScroll\} data-testid="messages">\s*<div ref=\{body\}>/);
  });
});

describe("one send, one server message (Fail 2)", () => {
  const LOCAL = (hms: string) => `2026-11-20T${hms}+08:00`;
  const attempt = () => ({ since: Date.parse("2026-11-20T11:59:55Z"), known: new Set<string>() });
  const msg = (id: string, hms: string): ChatMessage => ({ id, chat_session_id: "s", role: "user", content: "好的", task_id: null, created_at: LOCAL(hms) });
  const user = (m: ChatMessage) => m.role === "user";

  it("a retry does not take the same-text message another send is still waiting on", async () => {
    // 马云's steps: "好的" goes out with a slow answer (it lands as X); a second
    // "好的" fails and is retried as soon as it shows the failure.
    const server: ChatMessage[] = [];
    let answerA!: () => void;
    let posts = 0;
    const post = async () => {
      posts += 1;
      if (posts === 1) {
        server.push(msg("X", "20:00:01"));
        await new Promise<void>((r) => (answerA = r));
        return server[0]!;
      }
      if (posts === 2) throw new Error("connection reset");
      server.push(msg("Y", "20:00:09"));
      return server[1]!;
    };
    const deliver = sendQueue(async () => [...server], post, user);
    const a = deliver("好的", attempt(), false);
    const b = deliver("好的", attempt(), false);
    const retried = b.catch(() => deliver("好的", attempt(), true));
    setTimeout(() => answerA(), 20);
    const [ra, rb] = await Promise.all([a, retried]);
    expect(ra.item.id).toBe("X");
    expect(rb).toEqual({ item: server[1], earlier: false });
    expect(server.map((m) => m.id)).toEqual(["X", "Y"]);
  });

  it("a retry whose first try did land takes that one, once", async () => {
    // Both tries land, both answers get lost: each retry ends with its own one.
    const server: ChatMessage[] = [];
    let posts = 0;
    const post = async () => {
      posts += 1;
      server.push(msg(`M${posts}`, `20:00:0${posts}`));
      throw new Error("answer lost");
    };
    const deliver = sendQueue(async () => [...server], post, user);
    await expect(deliver("好的", attempt(), false)).rejects.toThrow();
    await expect(deliver("好的", attempt(), false)).rejects.toThrow();
    const r1 = await deliver("好的", attempt(), true);
    const r2 = await deliver("好的", attempt(), true);
    expect([r1.earlier, r2.earlier]).toEqual([true, true]);
    expect(new Set([r1.item.id, r2.item.id])).toEqual(new Set(["M1", "M2"]));
    expect(posts).toBe(2);
  });

  it("a different text does not wait for a slow send", async () => {
    let posted: string[] = [];
    const post = (text: string) => {
      posted = [...posted, text];
      return text === "慢" ? new Promise<ChatMessage>(() => {}) : Promise.resolve({ ...msg(`id-${text}`, "20:00:01"), content: text });
    };
    const deliver = sendQueue(async () => [], post, user);
    void deliver("慢", attempt(), false);
    expect((await deliver("快", attempt(), false)).item.id).toBe("id-快");
    expect(posted).toEqual(["慢", "快"]);
  });

  it("landed leaves out what another send already has (the comment box uses it too)", () => {
    expect(landed([msg("X", "20:00:01")], "好的", attempt(), user, new Set(["X"]))).toBeUndefined();
    expect(landed([msg("X", "20:00:01")], "好的", attempt(), user, new Set())?.id).toBe("X");
  });
});
