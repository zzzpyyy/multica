"use client";

import { useQuery } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import type { NestedChildGroup } from "@multica/core/issues";
import { childIssueProgressOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { AppLink } from "../../../navigation";
import { useT } from "../../../i18n";
import { ActorAvatar } from "../../../common/actor-avatar";
import { StatusIcon } from "../status-icon";
import { useStatusLabel } from "../../utils/status-label";
import { useIssuePeekLinkProps } from "../../surface/peek-context";

/**
 * Expanded list of a card's direct sub-issues (PRD A rules 4–5), grouped by
 * stage. Read-only. Rendered as a sibling of the card's link — not inside it —
 * so each title can be a real link that opens like a card does (A12).
 * Grandchildren are not expanded; a row only shows their closed/total (A5).
 */
export function NestedChildrenList({
  parent,
  groups,
  variant = "card",
}: {
  parent: Issue;
  groups: NestedChildGroup[];
  /** "card": the board card's lower half. "row": indented under a list row. */
  variant?: "card" | "row";
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: grandchildProgress } = useQuery(childIssueProgressOptions(wsId));
  // The card wrapper is a drag handle, a context-menu trigger and a Space-to-
  // peek target for the PARENT; none of that should fire from inside the list.
  const stop = (e: React.SyntheticEvent) => e.stopPropagation();
  return (
    <div
      role="list"
      aria-label={t(($) => $.nested.children_label, { identifier: parent.identifier })}
      onPointerDown={stop}
      onKeyDown={stop}
      onContextMenu={stop}
      className={
        variant === "card"
          ? "-mt-px rounded-b-lg border-[0.5px] border-t-0 border-surface-border bg-surface px-2.5 pb-2 shadow-[var(--surface-shadow)]"
          : "ml-14 mr-4 mb-1 border-l border-border pl-3 pb-1"
      }
    >
      {groups.map((group) => (
        <div key={group.stage ?? "none"} className="border-t border-border/60 pt-1.5 first:border-t-0">
          <p className="px-0.5 text-micro font-medium text-muted-foreground">
            {group.stage === null
              ? t(($) => $.nested.group_unstaged)
              : t(($) => $.nested.group_stage, { n: group.stage })}
          </p>
          {group.issues.map((child) => (
            <ChildRow key={child.id} issue={child} progress={grandchildProgress?.get(child.id)} />
          ))}
        </div>
      ))}
    </div>
  );
}

function ChildRow({
  issue,
  progress,
}: {
  issue: Issue;
  progress?: { done: number; total: number };
}) {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const peekLinkProps = useIssuePeekLinkProps(issue.id);
  const statusLabel = useStatusLabel(wsId);
  const { getActorName } = useActorName();
  const hasAssignee = !!issue.assignee_type && !!issue.assignee_id;
  return (
    <div role="listitem" className="py-1" data-child-issue={issue.identifier}>
      <div className="flex min-w-0 items-baseline gap-1.5">
        <span className="shrink-0 text-micro text-muted-foreground tabular-nums">{issue.identifier}</span>
        <AppLink
          href={p.issueDetail(issue.id)}
          newTabTitle={issue.identifier}
          className="min-w-0 truncate text-caption font-medium hover:underline"
          {...peekLinkProps}
        >
          {issue.title}
        </AppLink>
      </div>
      <div className="mt-0.5 flex min-w-0 items-center gap-2 text-micro text-muted-foreground">
        <span className="inline-flex shrink-0 items-center gap-1">
          <StatusIcon status={issue.status} category={issue.status_category} className="size-3" />
          {statusLabel(issue.status)}
        </span>
        {hasAssignee && (
          <span className="inline-flex min-w-0 items-center gap-1">
            <ActorAvatar
              actorType={issue.assignee_type!}
              actorId={issue.assignee_id!}
              size="xs"
              profileLink={false}
              className="shrink-0"
            />
            <span className="truncate">{getActorName(issue.assignee_type!, issue.assignee_id!)}</span>
          </span>
        )}
        {progress && progress.total > 0 && (
          <span className="ml-auto shrink-0 tabular-nums">
            {progress.done}/{progress.total}
          </span>
        )}
      </div>
    </div>
  );
}
