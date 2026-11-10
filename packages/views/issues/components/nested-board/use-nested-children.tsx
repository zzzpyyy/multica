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
import { childrenByParentsOptions } from "@multica/core/issues/queries";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";

interface NestedBoardValue {
  /** undefined until the first batch lands; [] for a card without children. */
  childrenOf: (parentId: string) => Issue[] | undefined;
  isExpanded: (parentId: string) => boolean;
  toggleExpanded: (parentId: string) => void;
}

const NestedBoardContext = createContext<NestedBoardValue | null>(null);
const NO_CHILDREN: Issue[] = [];

/**
 * Nested board (PRD A): one batched `GET /api/issues/children` for every card
 * on the board. The query stays ACTIVE — unlike the swimlane's enabled:false
 * observers — so the `childrenByParentsAll` invalidations in ws-updaters
 * actually refetch it (A6–A10).
 *
 * Expanded cards live here, not in the card: Virtuoso unmounts cards scrolled
 * out of view, and a reload should collapse everything (rule 4).
 */
export function NestedBoardProvider({
  issues,
  children,
}: {
  issues: readonly Issue[];
  children: ReactNode;
}) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const nested = useViewStore((s) => s.nestedSubIssues);
  const parentIds = useMemo(
    () => (nested ? [...new Set(issues.map((issue) => issue.id))].sort() : []),
    [nested, issues],
  );
  const { data } = useQuery({
    ...childrenByParentsOptions(wsId, parentIds, qc),
    // A new card changes the key; keep the old summaries up meanwhile.
    placeholderData: keepPreviousData,
  });
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
      isExpanded: (parentId) => expanded.has(parentId),
      toggleExpanded,
    }),
    [data, expanded, toggleExpanded],
  );
  if (!nested) return <>{children}</>;
  return <NestedBoardContext.Provider value={value}>{children}</NestedBoardContext.Provider>;
}

/** null outside a nested board — the card then renders exactly as upstream. */
export function useNestedBoard(): NestedBoardValue | null {
  return useContext(NestedBoardContext);
}
