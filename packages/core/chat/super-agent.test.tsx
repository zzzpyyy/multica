/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ApiError, setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { PlanCard } from "../types/super-agent";
import { superAgentKeys, superAgentOptions, useDecidePlanCard } from "./super-agent";

const card = (status: PlanCard["status"]): PlanCard => ({
  id: "card-1",
  chat_session_id: "sess-1",
  chat_message_id: "msg-1",
  status,
  error: null,
  created_at: "2026-11-10T00:00:00Z",
  kind: "create_issues",
  items: [],
  created_issues: [],
});

afterEach(() => vi.restoreAllMocks());

describe("superAgentOptions", () => {
  it("reads 409 runtime_required as not provisioned (null)", async () => {
    setApiInstance({
      bootstrapSuperAgent: vi.fn().mockRejectedValue(
        new ApiError("no runtime", 409, "Conflict", { code: "runtime_required" }),
      ),
    } as unknown as ApiClient);
    const qc = new QueryClient();
    await expect(qc.fetchQuery(superAgentOptions("ws-1"))).resolves.toBeNull();
  });

  it("surfaces any other failure as an error", async () => {
    setApiInstance({
      bootstrapSuperAgent: vi.fn().mockRejectedValue(new ApiError("nope", 404, "Not Found")),
    } as unknown as ApiClient);
    const qc = new QueryClient();
    await expect(qc.fetchQuery(superAgentOptions("ws-1"))).rejects.toBeInstanceOf(ApiError);
  });
});

describe("useDecidePlanCard", () => {
  function setup(decidePlanCard: ReturnType<typeof vi.fn>) {
    setApiInstance({ decidePlanCard } as unknown as ApiClient);
    const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    qc.setQueryData(superAgentKeys.planCards("sess-1"), [card("pending")]);
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useDecidePlanCard(), { wrapper });
    return { qc, result };
  }

  it("writes the returned card into the session's cards (B3, B5)", async () => {
    const { qc, result } = setup(vi.fn().mockResolvedValue(card("created")));
    await act(() => result.current.mutateAsync({ card: card("pending"), decision: "confirm" }));
    expect(qc.getQueryData<PlanCard[]>(superAgentKeys.planCards("sess-1"))?.[0]?.status).toBe("created");
  });

  it("adopts the card a 409 card_closed carries (B7: expired)", async () => {
    const { qc, result } = setup(
      vi.fn().mockRejectedValue(
        new ApiError("closed", 409, "Conflict", { code: "card_closed", card: card("expired") }),
      ),
    );
    await act(async () => {
      await result.current
        .mutateAsync({ card: card("pending"), decision: "confirm" })
        .catch(() => undefined);
    });
    expect(qc.getQueryData<PlanCard[]>(superAgentKeys.planCards("sess-1"))?.[0]?.status).toBe("expired");
  });
});
