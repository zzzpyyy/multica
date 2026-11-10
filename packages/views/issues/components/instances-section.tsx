"use client";

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitBranch } from "@multica/ui/icons";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueInstancesOptions } from "@multica/core/issues/queries";
import type { ListInstancesResponse, TaskInstance } from "@multica/core/types";
import { useT, useTimeAgo } from "../../i18n";

export interface TaskInstanceRef {
  index: number;
  total: number;
  split: string;
}

/** The instance a run belongs to, matched on any of its attempts (PRD D3). */
export function findTaskInstance(
  data: ListInstancesResponse,
  taskId: string,
): TaskInstanceRef | undefined {
  for (const batch of data.batches) {
    const instance = batch.instances.find(
      (i) => i.task_id === taskId || i.task_ids.includes(taskId),
    );
    if (instance) return { index: instance.index, total: batch.total, split: instance.split };
  }
  return undefined;
}

/** Reads the issue's one shared instances query, so every comment card costs no request. */
export function useTaskInstance(issueId: string, taskId: string): TaskInstanceRef | undefined {
  const wsId = useWorkspaceId();
  const select = useCallback(
    (data: ListInstancesResponse) => findTaskInstance(data, taskId),
    [taskId],
  );
  return useQuery({ ...issueInstancesOptions(wsId, issueId), select }).data;
}

/** "Instance i/k · <split>" beside the author of a comment an instance posted. */
export function InstanceLabel({ issueId, taskId }: { issueId: string; taskId: string }) {
  const { t } = useT("issues");
  const instance = useTaskInstance(issueId, taskId);
  if (!instance) return null;
  const label = `${t(($) => $.instances.index, { index: instance.index, total: instance.total })} · ${instance.split}`;
  return (
    <span
      className="inline-flex min-w-0 max-w-64 items-center rounded-sm bg-muted px-1.5 py-0.5 text-micro text-muted-foreground"
      title={label}
    >
      <span className="truncate">{label}</span>
    </span>
  );
}

/**
 * Read-only list of the role instances added on this issue (PRD D rule 9):
 * index, split, status, and for a queued one its place and reason. Adding
 * goes through the CLI or a plan card and cancelling through the CLI, so
 * there are no controls here. Renders nothing until a batch exists.
 */
export function InstancesSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data } = useQuery(issueInstancesOptions(wsId, issueId));
  const batches = data?.batches ?? [];
  if (batches.length === 0) return null;

  const statusText = (status: string) => {
    switch (status) {
      case "queued": return t(($) => $.execution_log.status_queued);
      case "running": return t(($) => $.execution_log.status_running);
      case "completed": return t(($) => $.execution_log.status_completed);
      case "failed": return t(($) => $.execution_log.status_failed);
      case "cancelled": return t(($) => $.execution_log.status_cancelled);
      default: return status;
    }
  };
  const reasonText = (queue: NonNullable<TaskInstance["queue"]>) => {
    switch (queue.reason) {
      case "role_full":
        return t(($) => $.instances.reason.role_full, { running: queue.running ?? "?", limit: queue.limit ?? "?" });
      case "runtime_full": return t(($) => $.instances.reason.runtime_full);
      case "runtime_offline": return t(($) => $.instances.reason.runtime_offline);
      case "waiting_dispatch": return t(($) => $.instances.reason.waiting_dispatch);
      default: return queue.reason;
    }
  };

  return (
    <div className="mt-10" data-testid="instances-section">
      <h3 className="mb-2 text-body font-medium">{t(($) => $.instances.section)}</h3>
      <div className="overflow-hidden rounded-lg border bg-card/30 divide-y divide-border/60">
        {batches.map((batch) => (
          <div key={batch.id} className="divide-y divide-border/60">
            <div className="flex items-center gap-1.5 bg-muted/40 px-3 py-1 text-micro text-muted-foreground">
              <span className="font-medium text-foreground">{batch.agent.name}</span>
              {batch.priority && <span>· {t(($) => $.instances.priority)}</span>}
              <span>· {timeAgo(batch.created_at)}</span>
            </div>
            {batch.instances.map((instance) => (
              <div key={instance.index} className="flex flex-col gap-0.5 px-3 py-2">
                <div className="flex items-start gap-2 text-label">
                  <span className="shrink-0 text-muted-foreground tabular-nums">
                    {t(($) => $.instances.index, { index: instance.index, total: batch.total })}
                  </span>
                  <span className="min-w-0 flex-1 line-clamp-2 break-words" title={instance.split}>
                    {instance.split}
                  </span>
                  <span className={instance.status === "failed" ? "shrink-0 text-destructive" : "shrink-0 text-muted-foreground"}>
                    {statusText(instance.status)}
                  </span>
                </div>
                {(instance.queue || instance.branch_name || instance.session_id) && (
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-micro text-muted-foreground">
                    {instance.status === "queued" && instance.queue && (
                      <span>
                        {t(($) => $.instances.queue_position, { position: instance.queue.position })}
                        {" · "}
                        {reasonText(instance.queue)}
                      </span>
                    )}
                    {instance.branch_name && (
                      <span className="inline-flex min-w-0 items-center gap-1 font-mono" title={instance.branch_name}>
                        <GitBranch className="size-3 shrink-0" aria-hidden="true" />
                        <span className="truncate">{instance.branch_name}</span>
                      </span>
                    )}
                    {instance.session_id && (
                      <span className="min-w-0 truncate font-mono" title={instance.session_id}>
                        {t(($) => $.instances.session, { id: instance.session_id })}
                      </span>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
