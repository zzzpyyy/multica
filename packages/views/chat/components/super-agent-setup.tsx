"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { superAgentOptions, useProvisionSuperAgent } from "@multica/core/chat/super-agent";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  MikaRuntimeChoice,
  type MikaRuntimeSelection,
} from "../../runtimes/components/mika-runtime-choice";
import { useT } from "../../i18n";

/**
 * One-time super-agent setup (F5): pick the Claude runtime it runs on, then
 * open its session. Renders only once the server has said "not provisioned"
 * (`null`); an older server without the endpoint leaves the query in error
 * and nothing shows.
 */
export function SuperAgentSetup() {
  const { t } = useT("chat");
  const wsId = useCurrentWorkspace()?.id ?? "";
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: superAgent } = useQuery({ ...superAgentOptions(wsId), enabled: !!wsId });
  const { data: runtimes = [], isLoading } = useQuery({
    ...runtimeListOptions(wsId),
    enabled: !!wsId && superAgent === null,
  });
  const provision = useProvisionSuperAgent(wsId);
  const [choice, setChoice] = useState<MikaRuntimeSelection | null>(null);
  if (superAgent !== null) return null;

  // The server refuses any other provider (`400 runtime_not_claude`).
  const claude = runtimes.filter((r) => r.provider === "claude");
  const value = choice ?? {
    runtimeId: (claude.find((r) => r.status === "online") ?? claude[0])?.id ?? "",
    model: "",
  };
  const start = () =>
    provision.mutate(
      { runtime_id: value.runtimeId, ...(value.model ? { model: value.model } : {}) },
      {
        onSuccess: (bootstrap) => {
          const chat = useChatStore.getState();
          chat.setSelectedAgentId(bootstrap.id);
          chat.setActiveSession(bootstrap.session.id);
        },
        onError: (err) =>
          toast.error(err instanceof Error ? err.message : t(($) => $.super_agent.setup_failed)),
      },
    );

  return (
    <div className="mx-3 mb-2 space-y-2.5 rounded-lg border border-surface-border bg-card p-3" data-super-agent-setup="">
      <div>
        <p className="text-body font-medium">{t(($) => $.super_agent.setup_title)}</p>
        <p className="mt-0.5 text-caption text-muted-foreground">{t(($) => $.super_agent.setup_description)}</p>
      </div>
      {claude.length === 0 && !isLoading ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.super_agent.setup_no_claude)}</p>
      ) : (
        <MikaRuntimeChoice
          runtimes={claude}
          runtimesLoading={isLoading}
          currentUserId={userId}
          value={value}
          onChange={setChoice}
          disabled={provision.isPending}
        />
      )}
      <Button size="sm" disabled={!value.runtimeId || provision.isPending} onClick={start}>
        {t(($) => $.super_agent.setup_action)}
      </Button>
    </div>
  );
}
