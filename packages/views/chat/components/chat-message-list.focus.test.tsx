import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ChatMessage } from "@multica/core/types";
import type { ReactElement } from "react";
import enChat from "../../locales/en/chat.json";
import { ChatMessageList } from "./chat-message-list";
import { resetRevealedLists } from "./stick-to-bottom";

// Fork (F7): `?message=` jumps to a message. The jump must release the
// bottom-stick; otherwise the next layout pass pins the list back to the
// newest message and the target is scrolled straight out of view.
const scrolls: unknown[] = [];
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    ref,
    data,
    itemContent,
    computeItemKey,
    followOutput,
  }: {
    ref?: { current: unknown };
    data: unknown[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
    followOutput?: (atBottom: boolean) => "smooth" | "auto" | false;
  }) => {
    if (ref) ref.current = { scrollToIndex: (location: unknown) => scrolls.push(location) };
    return (
      <div data-testid="rows" data-follow-at-bottom={String(followOutput?.(true))}>
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

afterEach(() => {
  scrolls.length = 0;
  resetRevealedLists();
  vi.useRealTimers();
});

describe("ChatMessageList ?message= focus", () => {
  it("scrolls to the message and stops following the live end", async () => {
    vi.useFakeTimers();
    const messages = ["a", "b", "c"].map((id, i) => message(id, i));
    render(
      <I18nProvider locale="en" resources={{ en: { chat: enChat } }}>
        <QueryClientProvider client={new QueryClient()}>
          <ChatMessageList messages={messages} pendingTask={null} availability={undefined} focusMessageId="a" />
        </QueryClientProvider>
      </I18nProvider>,
    );
    expect(screen.getByTestId("rows").dataset.followAtBottom).toBe("auto");
    // The reveal deadline marks the list ready, which triggers the jump.
    await act(async () => {
      vi.advanceTimersByTime(1100);
    });
    expect(scrolls).toContainEqual({ index: 0, align: "center" });
    expect(screen.getByTestId("rows").dataset.followAtBottom).toBe("false");
  });
});
