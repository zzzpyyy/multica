import {
  queryOptions,
  useMutation,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { api, ApiError, errorCode } from "../api";
import { workspaceKeys } from "../workspace/queries";
import type { Agent, ChatSession } from "../types";
import type { PlanCard, SuperAgentBootstrap } from "../types/super-agent";
import { chatKeys } from "./queries";

/** Mirrors the server's `SuperAgentSystemKey`. Identity is this key, never the name. */
export const SUPER_AGENT_SYSTEM_KEY = "super";

export function isSuperAgent(agent: Pick<Agent, "system_key"> | null | undefined): boolean {
  return agent?.system_key === SUPER_AGENT_SYSTEM_KEY;
}

/** True when `session` belongs to the super-agent (its one and only session). */
export function isSuperAgentSession(
  session: Pick<ChatSession, "agent_id"> | null | undefined,
  agents: readonly Agent[],
): boolean {
  return !!session && isSuperAgent(agents.find((a) => a.id === session.agent_id));
}

export const superAgentKeys = {
  bootstrap: (wsId: string) => [...chatKeys.all(wsId), "super-agent"] as const,
  planCardsAll: () => ["chat", "plan-cards"] as const,
  planCards: (sessionId: string) => [...superAgentKeys.planCardsAll(), sessionId] as const,
};

/**
 * The super-agent and this member's session with it, or null while the
 * workspace has none (`409 runtime_required`). The POST is idempotent and,
 * without a runtime_id, writes nothing — so reading it as a query is safe.
 * ⌘+J and `?agent=` take the session id only from here, never from the list.
 */
export function superAgentOptions(wsId: string) {
  return queryOptions({
    queryKey: superAgentKeys.bootstrap(wsId),
    queryFn: async (): Promise<SuperAgentBootstrap | null> => {
      try {
        return await api.bootstrapSuperAgent();
      } catch (err) {
        if (errorCode(err) === "runtime_required") return null;
        throw err;
      }
    },
    staleTime: Infinity,
    retry: false,
  });
}

/** First-time provisioning on a chosen Claude runtime. */
export function useProvisionSuperAgent(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { runtime_id: string; model?: string }) => api.bootstrapSuperAgent(data),
    onSuccess: async (bootstrap) => {
      qc.setQueryData(superAgentKeys.bootstrap(wsId), bootstrap);
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      // The chat surfaces drop an active session id the list does not know
      // yet; have the session in the list before anyone opens it.
      await qc.refetchQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

export function planCardsOptions(sessionId: string) {
  return queryOptions({
    queryKey: superAgentKeys.planCards(sessionId),
    queryFn: () => api.listPlanCards(sessionId).then((r) => r.cards),
    enabled: !!sessionId,
  });
}

function upsertPlanCard(qc: QueryClient, card: PlanCard) {
  qc.setQueryData<PlanCard[]>(superAgentKeys.planCards(card.chat_session_id), (old) =>
    old?.some((c) => c.id === card.id)
      ? old.map((c) => (c.id === card.id ? card : c))
      : [...(old ?? []), card],
  );
}

/**
 * Confirm or cancel a plan card. The server makes both idempotent; a card that
 * was already cancelled or expired answers `409 card_closed` with the card, which
 * is just as good an answer for the UI.
 */
export function useDecidePlanCard() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ card, decision }: { card: PlanCard; decision: "confirm" | "cancel" }) =>
      api.decidePlanCard(card.id, decision),
    onSuccess: (card) => upsertPlanCard(qc, card),
    onError: (err, { card }) => {
      const closed =
        err instanceof ApiError && errorCode(err) === "card_closed"
          ? (err.body as { card?: PlanCard } | undefined)?.card
          : undefined;
      if (closed) upsertPlanCard(qc, closed);
      else qc.invalidateQueries({ queryKey: superAgentKeys.planCards(card.chat_session_id) });
    },
  });
}
