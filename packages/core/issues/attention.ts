import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueAttentionResponse } from "../types";
import { issueKeys } from "./queries";

/** Per-issue counts for the one-line progress (PRD A rule 7). */
export interface IssueAttentionCounts {
  /** "N 项待你验收": in-review issues at any level BELOW this one. */
  awaitingReview: number;
  /** "m 项待你回复": this issue and every level below it. */
  awaitingReply: number;
}

/**
 * Folds the workspace-wide attention lists into counts per issue id. An
 * in-review issue counts for its ancestors only, never for itself; an issue
 * waiting for a reply counts for itself and its ancestors.
 */
export function attentionCountsByIssue(
  data: IssueAttentionResponse,
): Map<string, IssueAttentionCounts> {
  const counts = new Map<string, IssueAttentionCounts>();
  const bump = (id: string, field: keyof IssueAttentionCounts) => {
    const entry = counts.get(id) ?? { awaitingReview: 0, awaitingReply: 0 };
    entry[field] += 1;
    counts.set(id, entry);
  };
  for (const item of data.in_review) {
    for (const id of new Set(item.ancestor_ids)) bump(id, "awaitingReview");
  }
  for (const item of data.awaiting_reply) {
    for (const id of new Set([item.issue_id, ...item.ancestor_ids])) bump(id, "awaitingReply");
  }
  return counts;
}

export function issueAttentionOptions(wsId: string) {
  return queryOptions({
    queryKey: issueKeys.attention(wsId),
    queryFn: () => api.getIssueAttention(),
    select: attentionCountsByIssue,
  });
}

