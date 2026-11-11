import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ChatMessage } from "@multica/core/types";
import type { ReactElement } from "react";
import enChat from "../../locales/en/chat.json";
import { ChatMessageList } from "./chat-message-list";
import { resetRevealedLists } from "./stick-to-bottom";

// Fork (F7): `?message=` jumps to a message. The jump must release the
// bottom-stick, and the target must stay on screen while rows above it keep
// changing height after the jump (B18 retest: it drifted off, even to the top).
type Location = { index: number; align?: string };
const scrolls: Location[] = [];
// What followOutput answered at each render, as Virtuoso asks it on a count change.
const follows: { count: number; follow: string }[] = [];
let onScrollToIndex: (location: Location, data: ChatMessage[]) => void = () => {};
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    ref,
    data,
    itemContent,
    computeItemKey,
    followOutput,
  }: {
    ref?: { current: unknown };
    data: { message: ChatMessage }[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
    followOutput?: (atBottom: boolean) => "smooth" | "auto" | false;
  }) => {
    if (ref) {
      ref.current = {
        scrollToIndex: (location: Location) => {
          scrolls.push(location);
          onScrollToIndex(location, data.map((item) => item.message));
        },
      };
    }
    const follow = String(followOutput?.(true));
    follows.push({ count: data.length, follow });
    return (
      <div data-testid="rows" data-follow-at-bottom={follow}>
        {data.map((item, i) => (
          <div key={computeItemKey(i, item)}>{itemContent(i, item)}</div>
        ))}
      </div>
    );
  },
}));

vi.mock("../../rich-content", () => ({
  RichContent: ({ content }: { content: string }) => <span>{content}</span>,
}));

const message = (id: string, i: number): ChatMessage => ({
  id,
  chat_session_id: "s-1",
  role: "user",
  content: `m${i}`,
  task_id: null,
  created_at: `2026-11-11T00:00:0${i}Z`,
});

// Layout stub: the list viewport is 0–500px; each row sits at `rowTop`.
const ON_SCREEN = 200;
const OFF_SCREEN = 2000;
const rowTop = new Map<string, number>();
const box = (top: number, height: number) =>
  ({ top, bottom: top + height, height, left: 0, right: 0, width: 0, x: 0, y: top }) as DOMRect;

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "requestAnimationFrame", "cancelAnimationFrame", "Date"] });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    if (this.hasAttribute("data-tab-scroll-root")) return box(0, 500);
    const id = this.getAttribute("data-chat-message-id");
    return id && rowTop.has(id) ? box(rowTop.get(id)!, 50) : box(0, 0);
  });
  // A scrollToIndex lands its row on screen.
  onScrollToIndex = (location, data) => {
    const target = data[location.index];
    if (target) rowTop.set(target.id, ON_SCREEN);
  };
});

afterEach(() => {
  scrolls.length = 0;
  follows.length = 0;
  rowTop.clear();
  resetRevealedLists();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function renderList(messages: ChatMessage[]) {
  const client = new QueryClient();
  const ui = (list: ChatMessage[]) => (
    <I18nProvider locale="en" resources={{ en: { chat: enChat } }}>
      <QueryClientProvider client={client}>
        <ChatMessageList messages={list} pendingTask={null} availability={undefined} focusMessageId="a" />
      </QueryClientProvider>
    </I18nProvider>
  );
  const view = render(ui(messages));
  return { rerender: (list: ChatMessage[]) => view.rerender(ui(list)) };
}

const advance = (ms: number) =>
  act(async () => {
    vi.advanceTimersByTime(ms);
  });
const jumps = () => scrolls.filter((s) => s.align === "center");

async function jumpToA() {
  const messages = ["a", "b", "c"].map((id, i) => message(id, i));
  rowTop.set("a", OFF_SCREEN);
  const view = renderList(messages);
  expect(screen.getByTestId("rows").dataset.followAtBottom).toBe("auto");
  // The reveal deadline marks the list ready, which triggers the jump.
  await advance(1100);
  await advance(200);
  return view;
}

describe("ChatMessageList ?message= focus", () => {
  it("scrolls to the message and stops following the live end", async () => {
    await jumpToA();
    expect(jumps()).toEqual([{ index: 0, align: "center" }]);
    expect(screen.getByTestId("rows").dataset.followAtBottom).toBe("false");
  });

  it("does not re-issue while the row stays on screen", async () => {
    await jumpToA();
    await advance(3000);
    expect(jumps()).toHaveLength(1);
  });

  it("brings the row back when later layout pushes it off screen", async () => {
    await jumpToA();
    rowTop.set("a", -OFF_SCREEN); // rows above grew; the list ended up elsewhere
    await advance(200);
    expect(jumps()).toHaveLength(2);
    expect(rowTop.get("a")).toBe(ON_SCREEN);
  });

  it("follows the message to its new index when an older page is prepended", async () => {
    const view = await jumpToA();
    view.rerender(["z", "a", "b", "c"].map((id, i) => message(id, i)));
    rowTop.set("a", OFF_SCREEN);
    await advance(200);
    expect(jumps().at(-1)).toEqual({ index: 1, align: "center" });
  });

  it("lets go at the reader's first input", async () => {
    await jumpToA();
    window.dispatchEvent(new Event("wheel"));
    rowTop.set("a", OFF_SCREEN);
    await advance(1000);
    expect(jumps()).toHaveLength(1);
  });

  it("lets go after the hold window", async () => {
    await jumpToA();
    await advance(10_000);
    rowTop.set("a", OFF_SCREEN);
    await advance(1000);
    expect(jumps()).toHaveLength(1);
  });

  it("the older page that brings the message in does not follow the live end", async () => {
    // Virtuoso asks followOutput on every count change, prepends included,
    // before this list's effects run: the stick must already be released.
    const client = new QueryClient();
    const loadOlder = vi.fn();
    const ui = (list: ChatMessage[], fetching: boolean) => (
      <I18nProvider locale="en" resources={{ en: { chat: enChat } }}>
        <QueryClientProvider client={client}>
          <ChatMessageList
            messages={list}
            pendingTask={null}
            availability={undefined}
            focusMessageId="a"
            hasOlderMessages
            isFetchingOlderMessages={fetching}
            onLoadOlderMessages={loadOlder}
          />
        </QueryClientProvider>
      </I18nProvider>
    );
    const firstPage = ["b", "c"].map((id, i) => message(id, i + 1));
    const view = render(ui(firstPage, false));
    await advance(1100);
    expect(loadOlder).toHaveBeenCalledTimes(1);
    view.rerender(ui(firstPage, true));
    view.rerender(ui([message("a", 0), ...firstPage], false));
    // The render where the page lands, before any effect could release the stick.
    expect(follows.find((f) => f.count === 3)?.follow).toBe("false");
  });
});
