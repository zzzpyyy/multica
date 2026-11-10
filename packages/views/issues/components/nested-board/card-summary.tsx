"use client";

import type { NestedSummary } from "@multica/core/issues";
import { ChevronDown, ChevronRight } from "@multica/ui/icons";
import { useT } from "../../../i18n";

const chip = "inline-flex items-center rounded-full px-1.5 py-0.5 text-micro font-medium";

/**
 * Card row for the nested board (PRD A rule 3): cancelled count, stage k/n,
 * in-review and blocked counts, and the expand toggle. The "closed/total"
 * figure itself rides the card's existing progress ring. Nothing renders for
 * a parent without sub-issues (A3).
 */
export function NestedCardSummary({
  summary,
  expanded,
  onToggle,
}: {
  summary: NestedSummary;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { t } = useT("issues");
  if (summary.total === 0) return null;
  const { stage } = summary;
  // The card is one big link that is also a drag handle: keep the toggle's
  // pointer and click events from reaching either.
  const stop = (e: React.SyntheticEvent) => {
    e.stopPropagation();
    e.preventDefault();
  };
  return (
    <div className="mt-1.5 flex items-center gap-1.5" data-testid="nested-card-summary">
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
        {stage && (
          <span className={`${chip} bg-muted/60 text-muted-foreground`}>
            {stage === "all_done"
              ? t(($) => $.nested.stage_all_done)
              : t(($) => $.nested.stage, { current: stage.current, max: stage.max })}
          </span>
        )}
        {summary.inReview > 0 && (
          <span className={`${chip} bg-success/15 text-success`}>
            {t(($) => $.nested.in_review, { n: summary.inReview })}
          </span>
        )}
        {summary.blocked > 0 && (
          <span className={`${chip} bg-destructive/15 text-destructive`}>
            {t(($) => $.nested.blocked, { n: summary.blocked })}
          </span>
        )}
        {summary.cancelled > 0 && (
          <span className="text-micro text-muted-foreground">
            {t(($) => $.nested.cancelled, { n: summary.cancelled })}
          </span>
        )}
      </div>
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
    </div>
  );
}
