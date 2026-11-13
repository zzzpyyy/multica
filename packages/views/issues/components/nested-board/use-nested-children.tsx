"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueAttentionOptions,
  nestedSummary,
  progressSegments,
  type IssueAttentionCounts,
  type NestedSummary,
  type ProgressSegment,
} from "@multica/core/issues";
import { childrenByParentsOptions } from "@multica/core/issues/queries";
import { useStatusOrder } from "./card-summary";

interface NestedBoardValue {
  /** undefined until the first batch lands; [] for a card without children. */
  childrenOf: (parentId: string) => Issue[] | undefined;
  /** "待你验收""待你回复" for one card; undefined when nothing waits. */
  attentionOf: (issueId: string) => IssueAttentionCounts | undefined;
  isExpanded: (parentId: string) => boolean;
  toggleExpanded: (parentId: string) => void;
}

const NestedBoardContext = createContext<NestedBoardValue | null>(null);
const NO_CHILDREN: Issue[] = [];

/**
 * Nested board (PRD A): one batched `GET /api/issues/children` for every card
 * on the board, plus one `GET /api/issues/attention` for the whole workspace. The query stays ACTIVE — unlike the swimlane's enabled:false
 * observers — so the `childrenByParentsAll` invalidations in ws-updaters
 * actually refetch it (A6–A10).
 *
 * Expanded cards live here, not in the card: Virtuoso unmounts cards scrolled
 * out of view, and a reload should collapse everything (rule 4).
 */
export function NestedBoardProvider({
  issues,
  enabled,
  children,
}: {
  issues: readonly Issue[];
  /** The surface aggregates sub-issues (controller's `aggregatesSubIssues`). */
  enabled: boolean;
  children: ReactNode;
}) {
  // Off, a card or row renders exactly as upstream and nothing is fetched.
  if (!enabled) return <>{children}</>;
  return <ActiveNestedBoard issues={issues}>{children}</ActiveNestedBoard>;
}

function ActiveNestedBoard({ issues, children }: { issues: readonly Issue[]; children: ReactNode }) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const parentIds = useMemo(
    () => [...new Set(issues.map((issue) => issue.id))].sort(),
    [issues],
  );
  const { data } = useQuery({
    ...childrenByParentsOptions(wsId, parentIds, qc),
    // A new card changes the key; keep the old summaries up meanwhile.
    placeholderData: keepPreviousData,
  });
  // One workspace-wide request for every card (PRD A rule 7). An older server
  // without the endpoint errors here; the cards then show no attention counts.
  const { data: attention } = useQuery(issueAttentionOptions(wsId));
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const toggleExpanded = useCallback((parentId: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (!next.delete(parentId)) next.add(parentId);
      return next;
    });
  }, []);
  const value = useMemo<NestedBoardValue>(
    () => ({
      childrenOf: (parentId) => (data ? data.get(parentId) ?? NO_CHILDREN : undefined),
      attentionOf: (issueId) => attention?.get(issueId),
      isExpanded: (parentId) => expanded.has(parentId),
      toggleExpanded,
    }),
    [data, attention, expanded, toggleExpanded],
  );
  return <NestedBoardContext.Provider value={value}>{children}</NestedBoardContext.Provider>;
}

/** null outside a nested board — the card then renders exactly as upstream. */
export function useNestedBoard(): NestedBoardValue | null {
  return useContext(NestedBoardContext);
}

/** What a card or list row shows for its sub-issues (PRD A rules 3, 7, 8). */
export interface NestedIssue {
  summary: NestedSummary;
  segments: ProgressSegment[];
  expanded: boolean;
  onToggle: () => void;
}

/**
 * One parent's nested state for a board card or list row. `nested` is
 * undefined outside an aggregating surface, or until the children batch
 * lands. `progress` is the ring: the same direct children the summary counts
 * (A13), falling back to the workspace progress query (same done + cancelled
 * reading) until the batch lands.
 */
export function useNestedIssue(
  issue: Issue,
  fallbackProgress?: { done: number; total: number },
): { nested: NestedIssue | undefined; progress: { done: number; total: number } | undefined } {
  const board = useNestedBoard();
  const children = board?.childrenOf(issue.id);
  const attention = board?.attentionOf(issue.id);
  const statusOrder = useStatusOrder();
  const summary = useMemo(() => (children ? nestedSummary(children) : null), [children]);
  const segments = useMemo(
    () =>
      children
        ? progressSegments(children, {
            parentIdentifier: issue.identifier,
            statusOrder,
            awaitingReview: attention?.awaitingReview,
            awaitingReply: attention?.awaitingReply,
          })
        : [],
    [children, issue.identifier, statusOrder, attention],
  );
  const expanded = !!summary && summary.total > 0 && !!board?.isExpanded(issue.id);
  const nested = useMemo(
    () =>
      summary && board
        ? { summary, segments, expanded, onToggle: () => board.toggleExpanded(issue.id) }
        : undefined,
    [summary, segments, expanded, board, issue.id],
  );
  const progress = summary
    ? summary.total > 0
      ? { done: summary.closed, total: summary.total }
      : undefined
    : fallbackProgress;
  return { nested, progress };
}
