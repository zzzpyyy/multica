import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { nestedSummary } from "@multica/core/issues";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import type { NestedIssue } from "./nested-board/use-nested-children";
import { BoardCardContent } from "./board-card";

// Fork (PRD A, 一鸣 13 Nov 12:02): on a parent card the one-line progress has
// a single row under it, which carries "含 N 个已取消", "展开" and the ring.
// A card without sub-issues looks as before. Widths are checked in a real
// browser (BUNN-31); this pins which row each piece is in.
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/properties", () => ({ propertyListOptions: () => ({ queryKey: ["properties"] }) }));
vi.mock("@multica/core/issue-statuses/hooks", async () => {
  const { buildIssueStatusCatalog } = await import("@multica/core/issue-statuses/queries");
  return { useIssueStatuses: () => buildIssueStatusCatalog([]) };
});
let cardProperties: Record<string, boolean> = { assignee: true, dueDate: true, childProgress: true };
vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector: (state: unknown) => unknown) =>
    selector({ viewMode: "board", grouping: "status", swimlaneGrouping: "assignee", cardProperties, cardPropertyIds: [] }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "qa2" }) }));
vi.mock("../actions", () => ({ IssueActionsContextMenu: ({ children }: { children: ReactNode }) => children }));
vi.mock("./issue-agent-activity-indicator", () => ({ IssueAgentActivityIndicator: () => null }));
vi.mock("./issue-agent-run-counts", () => ({ IssueAgentRunCounts: () => null }));
vi.mock("./custom-status-chip", () => ({ CustomStatusChip: () => null, useIsCustomStatus: () => false }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span data-testid="avatar" /> }));

const issue = (extra: Partial<Issue> = {}) =>
  ({
    id: "p1", identifier: "BUNN-101", title: "P1", description: null, status: "todo", priority: "none",
    assignee_type: null, assignee_id: null, labels: [], properties: {}, start_date: null, due_date: null,
    updated_at: "2026-11-13T00:00:00Z", ...extra,
  }) as unknown as Issue;
const child = (status: string, stage: number) => ({ id: `${status}-${stage}`, number: stage, status, stage }) as Issue;

function nestedP1(): NestedIssue {
  const children = [child("done", 1), child("cancelled", 1), child("in_review", 2), child("blocked", 2), child("backlog", 3)];
  return {
    summary: nestedSummary(children),
    segments: [
      { kind: "stage", current: 2, max: 3 },
      { kind: "current", status: "in_review", title: "接口联调" },
      { kind: "blocked", n: 1 },
      { kind: "awaiting_review", n: 1 },
    ],
    expanded: false,
    onToggle: vi.fn(),
  };
}

/** The element right under the progress line's block, and how many follow it. */
function rowsUnderLine() {
  const block = screen.getByTestId("nested-card-summary");
  const after: Element[] = [];
  for (let el = block.nextElementSibling; el; el = el.nextElementSibling) after.push(el);
  return after;
}

describe("BoardCardContent on the nested board", () => {
  it("a parent card has one row under its line: cancelled count, 展开 and the ring", () => {
    renderWithI18n(<BoardCardContent issue={issue()} childProgress={{ done: 2, total: 5 }} nested={nestedP1()} />, {
      locale: "zh-Hans",
    });
    const rows = rowsUnderLine();
    expect(rows).toHaveLength(1);
    const row = rows[0]!;
    expect(row).toContainElement(screen.getByText("含 1 个已取消"));
    expect(row).toContainElement(screen.getByRole("button", { name: "展开" }));
    expect(row).toContainElement(screen.getByText("2/5"));
    expect(screen.getByTestId("nested-progress-line")).not.toContainElement(screen.getByText("含 1 个已取消"));
  });

  it("keeps the assignee and due date on that same row", () => {
    renderWithI18n(
      <BoardCardContent
        issue={issue({ assignee_type: "member", assignee_id: "u1", due_date: "2026-11-20" } as Partial<Issue>)}
        childProgress={{ done: 2, total: 5 }}
        nested={nestedP1()}
      />,
      { locale: "zh-Hans" },
    );
    const rows = rowsUnderLine();
    expect(rows).toHaveLength(1);
    expect(rows[0]).toContainElement(screen.getByTestId("avatar"));
    expect(rows[0]).toContainElement(screen.getByRole("button", { name: "展开" }));
    expect(rows[0]).toContainElement(screen.getByText("2/5"));
    // Short of room the cancelled count wraps whole onto a clipped second line
    // (never "含 …"), and the ring's title still says it.
    const cancelled = screen.getByTestId("nested-cancelled");
    expect(cancelled.className).toMatch(/\bh-lh\b/);
    expect(cancelled.className).toMatch(/\bflex-wrap\b/);
    expect(cancelled.className).toMatch(/\boverflow-hidden\b/);
    expect(cancelled.className).not.toMatch(/truncate/);
    // An empty first line would be 0 tall and leave the "clipped" line on screen.
    expect(cancelled.firstElementChild?.className).toMatch(/\bh-lh\b/);
    expect(screen.getByText("2/5").closest("[title]")).toHaveAttribute("title", "含 1 个已取消");
  });

  it("still shows 展开 when the ring is switched off in Display", () => {
    cardProperties = { assignee: false, dueDate: false, childProgress: false };
    try {
      renderWithI18n(<BoardCardContent issue={issue()} childProgress={{ done: 2, total: 5 }} nested={nestedP1()} />, {
        locale: "zh-Hans",
      });
      const rows = rowsUnderLine();
      expect(rows).toHaveLength(1);
      expect(rows[0]).toContainElement(screen.getByRole("button", { name: "展开" }));
      expect(screen.queryByText("2/5")).toBeNull();
    } finally {
      cardProperties = { assignee: true, dueDate: true, childProgress: true };
    }
  });

  it("a card without sub-issues shows only its line (A21's P2), no toggle", () => {
    const view = renderWithI18n(
      <BoardCardContent
        issue={issue()}
        nested={{ summary: nestedSummary([]), segments: [{ kind: "awaiting_reply", n: 1 }], expanded: false, onToggle: vi.fn() }}
      />,
      { locale: "zh-Hans" },
    );
    expect(screen.getByTestId("nested-progress-line").textContent).toBe("1 项待你回复");
    expect(screen.queryByRole("button")).toBeNull();
    expect(rowsUnderLine()).toHaveLength(0);
    view.unmount();
  });
});
