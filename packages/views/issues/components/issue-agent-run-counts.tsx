"use client";

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { AgentTask } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { useT } from "../../i18n";
import { selectIssueTasks } from "../surface/activity";

export interface AgentRunCount {
  agentId: string;
  running: number;
  queued: number;
}

// The server's reading of a role's limit N (CountRunningTasks) and of an
// instance's "running": a claimed run holds its slot before it starts.
// selectIssueTasks files dispatched / waiting_local_directory under queued.
const HOLDS_SLOT: ReadonlySet<AgentTask["status"]> = new Set(["running", "dispatched", "waiting_local_directory"]);

/** One issue's active runs counted per agent (PRD D rule 9, D5). */
export function issueAgentRunCounts(snapshot: readonly AgentTask[], issueId: string): AgentRunCount[] {
  const { running, queued } = selectIssueTasks(snapshot, issueId);
  const byAgent = new Map<string, AgentRunCount>();
  for (const task of [...running, ...queued]) {
    const row = byAgent.get(task.agent_id) ?? { agentId: task.agent_id, running: 0, queued: 0 };
    if (HOLDS_SLOT.has(task.status)) row.running++;
    else row.queued++;
    byAgent.set(task.agent_id, row);
  }
  return [...byAgent.values()].sort((a, b) => a.agentId.localeCompare(b.agentId));
}

/** "<agent> running m · queued q" lines on a board card; nothing when idle. */
export function IssueAgentRunCounts({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const select = useCallback(
    (snapshot: AgentTask[]) => issueAgentRunCounts(snapshot, issueId),
    [issueId],
  );
  const { data: rows = [] } = useQuery({ ...agentTaskSnapshotOptions(wsId), select });
  if (rows.length === 0) return null;
  return (
    <div className="mt-1.5 flex flex-col text-micro text-muted-foreground tabular-nums" data-testid="agent-run-counts">
      {rows.map((row) => (
        <span key={row.agentId} className="truncate">
          {t(($) => $.instances.card_counts, {
            agent: getActorName("agent", row.agentId),
            running: row.running,
            queued: row.queued,
          })}
        </span>
      ))}
    </div>
  );
}
