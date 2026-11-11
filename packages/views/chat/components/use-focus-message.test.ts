import { describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { FOCUS_MESSAGE_MAX_PAGES, useFocusMessage } from "./use-focus-message";

type Props = Parameters<typeof useFocusMessage>[0];
const base = (over: Partial<Props> = {}): Props => ({
  messageId: "m-1",
  index: -1,
  loadedCount: 50,
  ready: true,
  hasOlder: false,
  isFetchingOlder: false,
  loadOlder: vi.fn(),
  scrollTo: vi.fn(),
  onMissing: vi.fn(),
  ...over,
});

describe("useFocusMessage (F7, B18)", () => {
  it("scrolls to a loaded message once and highlights it", () => {
    const props = base({ index: 7 });
    const { result, rerender } = renderHook((p: Props) => useFocusMessage(p), { initialProps: props });
    expect(props.scrollTo).toHaveBeenCalledWith(7, "m-1");
    expect(result.current).toBe("m-1");
    rerender({ ...props, index: 9 });
    expect(props.scrollTo).toHaveBeenCalledTimes(1);
  });

  it("waits until the list is ready", () => {
    const props = base({ index: 3, ready: false });
    const { rerender } = renderHook((p: Props) => useFocusMessage(p), { initialProps: props });
    expect(props.scrollTo).not.toHaveBeenCalled();
    rerender({ ...props, ready: true });
    expect(props.scrollTo).toHaveBeenCalledWith(3, "m-1");
  });

  it("walks older pages until the message loads", () => {
    const props = base({ hasOlder: true });
    const { rerender } = renderHook((p: Props) => useFocusMessage(p), { initialProps: props });
    expect(props.loadOlder).toHaveBeenCalledTimes(1);
    rerender({ ...props, isFetchingOlder: true });
    // Re-renders before the fetch flag flips (or with a new loadOlder
    // identity) must not request the same page again.
    rerender({ ...props, loadOlder: props.loadOlder });
    rerender({ ...props });
    expect(props.loadOlder).toHaveBeenCalledTimes(1);
    rerender({ ...props, loadedCount: 100, index: 0 });
    expect(props.scrollTo).toHaveBeenCalledWith(0, "m-1");
    expect(props.onMissing).not.toHaveBeenCalled();
  });

  it("gives up after the page cap, or when history runs out", () => {
    const capped = base({ hasOlder: true });
    const { rerender } = renderHook((p: Props) => useFocusMessage(p), { initialProps: capped });
    for (let i = 1; i <= FOCUS_MESSAGE_MAX_PAGES + 2; i += 1) {
      rerender({ ...capped, isFetchingOlder: true });
      rerender({ ...capped, isFetchingOlder: false, loadedCount: 50 + i * 50 });
    }
    expect(capped.loadOlder).toHaveBeenCalledTimes(FOCUS_MESSAGE_MAX_PAGES);
    expect(capped.onMissing).toHaveBeenCalledTimes(1);

    const exhausted = base();
    renderHook(() => useFocusMessage(exhausted));
    expect(exhausted.onMissing).toHaveBeenCalledTimes(1);
  });

  it("does nothing without a message id", () => {
    const props = base({ messageId: null, index: 2 });
    renderHook(() => useFocusMessage(props));
    expect(props.scrollTo).not.toHaveBeenCalled();
    expect(props.onMissing).not.toHaveBeenCalled();
  });
});
