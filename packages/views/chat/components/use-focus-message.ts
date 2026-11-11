import { useEffect, useRef, useState } from "react";

/** Older pages to walk (100 messages each) before giving up. */
export const FOCUS_MESSAGE_MAX_PAGES = 20;
const HIGHLIGHT_MS = 2500;

/**
 * Fork (F7, BUNN-28 §3.6): `?message=<id>` on the chat page scrolls to that
 * message and briefly highlights it. A message older than the loaded pages is
 * reached by walking older pages, up to FOCUS_MESSAGE_MAX_PAGES; past that, or
 * when history runs out, `onMissing` fires once. Runs once per mount — the list
 * remounts per session.
 *
 * `index` is the message's row index in the list data, -1 while not loaded.
 * `loadedCount` is how many rows are loaded: one older page is requested per
 * growth, so re-renders while a page is in flight never spend the budget.
 * Returns the id to highlight right now.
 */
export function useFocusMessage({
  messageId,
  index,
  loadedCount,
  ready,
  hasOlder,
  isFetchingOlder,
  loadOlder,
  scrollTo,
  onMissing,
}: {
  messageId: string | null | undefined;
  index: number;
  loadedCount: number;
  ready: boolean;
  hasOlder: boolean;
  isFetchingOlder: boolean;
  loadOlder?: () => void;
  scrollTo: (index: number) => void;
  onMissing?: () => void;
}): string | null {
  const done = useRef(false);
  const pages = useRef(0);
  const requestedAt = useRef<number | null>(null);
  const [highlighted, setHighlighted] = useState<string | null>(null);

  useEffect(() => {
    if (!messageId || done.current || !ready || isFetchingOlder) return;
    if (index >= 0) {
      done.current = true;
      scrollTo(index);
      setHighlighted(messageId);
    } else if (hasOlder && loadOlder && pages.current < FOCUS_MESSAGE_MAX_PAGES) {
      if (requestedAt.current === loadedCount) return; // that page has not landed yet
      requestedAt.current = loadedCount;
      pages.current += 1;
      loadOlder();
    } else {
      done.current = true;
      onMissing?.();
    }
  }, [messageId, index, loadedCount, ready, hasOlder, isFetchingOlder, loadOlder, scrollTo, onMissing]);

  useEffect(() => {
    if (!highlighted) return;
    const timer = setTimeout(() => setHighlighted(null), HIGHLIGHT_MS);
    return () => clearTimeout(timer);
  }, [highlighted]);

  return highlighted;
}
