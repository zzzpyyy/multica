/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";
import type { WSClient } from "../api/ws-client";
import { issueKeys } from "../issues/queries";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

// Fork (PRD A21–A23): every comment event refetches the attention lists.
vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1",
  getCurrentSlug: () => "test-ws",
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));
vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

it.each([
  ["comment:created", { comment: { id: "c1", issue_id: "issue-1" } }],
  ["comment:updated", { comment: { id: "c1", issue_id: "issue-1" }, issue_revision: 2 }],
  ["comment:deleted", { comment_id: "c1", issue_id: "issue-1", issue_revision: 3 }],
])("%s invalidates the attention lists", (event, payload) => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const key = issueKeys.attention("ws-1");
  qc.setQueryData(key, { awaiting_reply: [], in_review: [] });
  const handlers: Record<string, (p: unknown) => void> = {};
  const ws = {
    on: vi.fn((name: string, handler: (p: unknown) => void) => {
      handlers[name] = handler;
      return () => {};
    }),
    onAny: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  } as unknown as WSClient;
  const stores = {
    authStore: Object.assign(() => ({}), {
      getState: () => ({ user: { id: "u1" } }),
      subscribe: () => () => {},
      setState: () => {},
      destroy: () => {},
    }),
  } as unknown as RealtimeSyncStores;
  renderHook(() => useRealtimeSync(ws, stores), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
  handlers[event]?.(payload);
  expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
});
