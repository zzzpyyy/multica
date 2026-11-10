"use client";

import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import {
  planCardsOptions,
  superAgentOptions,
  useDecidePlanCard,
} from "@multica/core/chat/super-agent";
import type { ChatMessage } from "@multica/core/types";
import type { PlanCard, PlanCardStatus } from "@multica/core/types/super-agent";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Plan cards the super-agent attached to this assistant message (PRD B rules
 * 2, 5, 10; BUNN-28 §3.1–3.2). Read from `GET plan-cards` so their state
 * survives a refresh; nothing renders outside the super-agent's session.
 */
export function PlanCardsForMessage({ message }: { message: ChatMessage }) {
  // Null-safe: the message list also renders outside a workspace route (tests, previews).
  const wsId = useCurrentWorkspace()?.id ?? "";
  const { data: superAgent } = useQuery({ ...superAgentOptions(wsId), enabled: !!wsId });
  const isSuperSession = !!superAgent && superAgent.session.id === message.chat_session_id;
  const { data: cards } = useQuery({
    ...planCardsOptions(message.chat_session_id),
    enabled: isSuperSession,
  });
  const mine = cards?.filter((card) => card.chat_message_id === message.id) ?? [];
  if (!isSuperSession || mine.length === 0) return null;
  return (
    <div className="space-y-2">
      {mine.map((card) => (
        <PlanCardView key={card.id} card={card} />
      ))}
    </div>
  );
}

const STATUS_TONE: Record<PlanCardStatus, string> = {
  pending: "bg-brand/10 text-brand",
  creating: "bg-muted text-muted-foreground",
  created: "bg-success/15 text-success",
  failed: "bg-destructive/15 text-destructive",
  cancelled: "bg-muted text-muted-foreground",
  expired: "bg-muted text-muted-foreground",
};

export function PlanCardView({ card }: { card: PlanCard }) {
  const { t } = useT("chat");
  const { t: tIssues } = useT("issues");
  const paths = useWorkspacePaths();
  const decide = useDecidePlanCard();
  const statusLabel: Record<PlanCardStatus, string> = {
    pending: t(($) => $.super_agent.status_pending),
    creating: t(($) => $.super_agent.status_creating),
    created: t(($) => $.super_agent.status_created),
    failed: t(($) => $.super_agent.status_failed),
    cancelled: t(($) => $.super_agent.status_cancelled),
    expired: t(($) => $.super_agent.status_expired),
  };
  const busy = decide.isPending || card.status === "creating";
  const issueLink = (issue: { id: string; identifier: string }) => (
    <AppLink key={issue.id} href={paths.issueDetail(issue.id)} className="font-medium text-brand hover:underline">
      {issue.identifier}
    </AppLink>
  );

  return (
    <div
      className="rounded-lg border border-surface-border bg-surface px-3 py-2.5 text-body"
      data-plan-card={card.id}
      data-status={card.status}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-caption font-medium text-muted-foreground">
          {card.kind === "create_issues"
            ? t(($) => $.super_agent.plan_create_title)
            : t(($) => $.super_agent.plan_instances_title)}
        </span>
        <span className={cn("rounded-full px-1.5 py-0.5 text-micro font-medium", STATUS_TONE[card.status])}>
          {statusLabel[card.status]}
        </span>
      </div>

      {card.kind === "create_issues" ? (
        <ol className="mt-2 space-y-2.5">
          {card.items.map((item, i) => (
            <li key={i} className="space-y-1">
              <p className="font-medium">{item.title}</p>
              <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-2 gap-y-0.5 text-caption">
                <dt className="text-muted-foreground">{t(($) => $.super_agent.field_assignee)}</dt>
                <dd>{item.assignee.name}</dd>
                <dt className="text-muted-foreground">{t(($) => $.super_agent.field_what)}</dt>
                <dd className="whitespace-pre-wrap">{item.what}</dd>
                <dt className="text-muted-foreground">{t(($) => $.super_agent.field_done)}</dt>
                <dd className="whitespace-pre-wrap">{item.done}</dd>
                <dt className="text-muted-foreground">{t(($) => $.super_agent.field_status)}</dt>
                <dd>
                  {tIssues(($) => $.status[item.status])}
                  {" · "}
                  <span className={item.will_run ? "font-medium text-warning" : "text-muted-foreground"}>
                    {item.will_run ? t(($) => $.super_agent.will_run) : t(($) => $.super_agent.wont_run)}
                  </span>
                </dd>
              </dl>
            </li>
          ))}
        </ol>
      ) : (
        <div className="mt-2 space-y-1 text-caption">
          <p>
            {t(($) => $.super_agent.instances_on, { n: card.splits.length, agent: card.agent.name })}{" "}
            {issueLink(card.issue)} {card.issue.title}
            {card.priority && (
              <span className="ml-1.5 rounded-full bg-warning/15 px-1.5 py-0.5 text-micro font-medium text-warning">
                {t(($) => $.super_agent.priority)}
              </span>
            )}
          </p>
          <ol className="list-decimal space-y-0.5 pl-5">
            {card.splits.map((split, i) => (
              <li key={i} className="whitespace-pre-wrap">{split}</li>
            ))}
          </ol>
        </div>
      )}

      {card.status === "pending" ? (
        <div className="mt-2.5 flex items-center gap-2">
          <Button
            size="sm"
            disabled={busy}
            onClick={() => decide.mutate({ card, decision: "confirm" })}
          >
            {card.kind === "create_issues"
              ? t(($) => $.super_agent.confirm_create)
              : t(($) => $.super_agent.confirm_instances)}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onClick={() => decide.mutate({ card, decision: "cancel" })}
          >
            {t(($) => $.super_agent.cancel)}
          </Button>
          {decide.isError && (
            <span className="text-caption text-destructive">{t(($) => $.super_agent.decide_failed)}</span>
          )}
        </div>
      ) : card.kind === "create_issues" && card.created_issues.length > 0 ? (
        <p className="mt-2 flex flex-wrap items-center gap-1.5 text-caption">
          <span className="text-muted-foreground">
            {card.status === "failed" ? t(($) => $.super_agent.failed_partial) : t(($) => $.super_agent.created_issues)}
          </span>
          {card.created_issues.map(issueLink)}
        </p>
      ) : card.kind === "add_instances" && card.status === "created" ? (
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.super_agent.added_instances)} {issueLink(card.issue)}
        </p>
      ) : null}
      {card.status === "failed" && card.error && (
        <p className="mt-1 text-caption text-destructive">{card.error}</p>
      )}
    </div>
  );
}
