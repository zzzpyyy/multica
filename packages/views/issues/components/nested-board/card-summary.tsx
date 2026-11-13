"use client";

import { Fragment, useCallback, useMemo } from "react";
import { BUILT_IN_STATUS_ORDER, type NestedSummary, type ProgressSegment } from "@multica/core/issues";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { ChevronDown, ChevronRight } from "@multica/ui/icons";
import { useT } from "../../../i18n";
import { useStatusLabel } from "../../utils/status-label";

const SEPARATOR = " · ";

/** The two segments that ask something of the viewer (PRD A rule 7). */
const isCallToAction = (segment: ProgressSegment) =>
  segment.kind === "awaiting_review" || segment.kind === "awaiting_reply";

/** Status keys in board column order, for "x 个进行中、y 个审核中". */
export function useStatusOrder(): readonly string[] {
  const { statuses } = useIssueStatuses(useWorkspaceId());
  return useMemo(
    () => (statuses.length > 0 ? statuses.map((entry) => entry.key) : BUILT_IN_STATUS_ORDER),
    [statuses],
  );
}

/** Renders one progress segment in the UI language. */
export function useProgressText() {
  const { t } = useT("issues");
  const statusLabel = useStatusLabel(useWorkspaceId());
  return useCallback(
    (segment: ProgressSegment): string => {
      switch (segment.kind) {
        case "stage":
          return t(($) => $.nested.stage, { current: segment.current, max: segment.max });
        case "stage_all_done":
          return t(($) => $.nested.stage_all_done);
        case "children_all_done":
          return t(($) => $.nested.children_all_done);
        case "current":
          return t(($) => $.nested.progress_current, {
            status: statusLabel(segment.status),
            title: segment.title,
          });
        case "current_counts":
          return segment.counts
            .map(({ status, n }) => t(($) => $.nested.progress_status_count, { n, status: statusLabel(status) }))
            .join(t(($) => $.nested.progress_count_separator));
        case "blocked":
          return t(($) => $.nested.progress_blocked, { n: segment.n });
        case "awaiting_review":
          return t(($) => $.nested.progress_awaiting_review, { n: segment.n });
        case "awaiting_reply":
          return t(($) => $.nested.progress_awaiting_reply, { n: segment.n });
      }
    },
    [t, statusLabel],
  );
}

/**
 * The one-line progress (PRD A rule 7): segments joined by " · ", the two
 * calls to action in a warning color. Nothing renders for no segments.
 */
export function ProgressLine({
  segments,
  className = "min-w-0 flex-1",
}: {
  segments: readonly ProgressSegment[];
  className?: string;
}) {
  const text = useProgressText();
  if (segments.length === 0) return null;
  return (
    <span className={`${className} text-micro text-muted-foreground`} data-testid="nested-progress-line">
      {segments.map((segment, i) => (
        <Fragment key={segment.kind}>
          {i > 0 && SEPARATOR}
          <span className={isCallToAction(segment) ? "font-medium text-warning" : undefined}>
            {text(segment)}
          </span>
        </Fragment>
      ))}
    </span>
  );
}

/**
 * "展开 / 收起" for a parent's sub-issues. It sits inside a link that is also
 * a drag handle: keep its pointer and click events from reaching either.
 */
export function NestedToggle({ expanded, onToggle }: { expanded: boolean; onToggle: () => void }) {
  const { t } = useT("issues");
  const stop = (e: React.SyntheticEvent) => {
    e.stopPropagation();
    e.preventDefault();
  };
  return (
    <button
      type="button"
      aria-expanded={expanded}
      onPointerDown={stop}
      onMouseDown={stop}
      onClick={(e) => {
        stop(e);
        onToggle();
      }}
      onKeyDown={(e) => e.stopPropagation()}
      className="inline-flex shrink-0 items-center gap-0.5 rounded-xs px-1 py-0.5 text-micro text-muted-foreground hover:bg-muted/60 hover:text-foreground"
    >
      {expanded ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
      {expanded ? t(($) => $.nested.collapse) : t(($) => $.nested.expand)}
    </button>
  );
}

/**
 * Card row for the nested board: the one-line progress, the cancelled count
 * and the expand toggle. The "closed/total" figure itself rides the card's
 * existing progress ring. A parent without sub-issues shows only what waits
 * on the viewer (A21's P2), and nothing at all otherwise (A3).
 */
export function NestedCardSummary({
  summary,
  segments,
  expanded,
  onToggle,
}: {
  summary: NestedSummary;
  segments: readonly ProgressSegment[];
  expanded: boolean;
  onToggle: () => void;
}) {
  const { t } = useT("issues");
  if (summary.total === 0 && segments.length === 0) return null;
  return (
    <div className="mt-1.5 flex items-start gap-1.5" data-testid="nested-card-summary">
      <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-1.5">
        <ProgressLine segments={segments} />
        {summary.cancelled > 0 && (
          <span className="text-micro text-muted-foreground">
            {t(($) => $.nested.cancelled, { n: summary.cancelled })}
          </span>
        )}
      </div>
      {summary.total > 0 && <NestedToggle expanded={expanded} onToggle={onToggle} />}
    </div>
  );
}
