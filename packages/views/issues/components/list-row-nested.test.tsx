import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { nestedSummary } from "@multica/core/issues";
import type { Issue } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import type { NestedIssue } from "./nested-board/use-nested-children";
import { ListRow } from "./list-row";

// Fork (PRD A16): on an aggregating list a parent row carries the ring, the
// one-line progress, the cancelled count and "展开"; expanded, its direct
// sub-issues are listed under it. The board card's equivalents are tested in
// nested-board/card-summary.test.tsx.
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/properties", () => ({ propertyListOptions: () => ({ queryKey: ["properties"] }) }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector: (state: unknown) => unknown) =>
    selector({ cardProperties: { childProgress: true }, cardPropertyIds: [] }),
}));
vi.mock("../actions", () => ({
  IssueActionsContextMenu: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("./issue-agent-activity-indicator", () => ({ IssueAgentActivityIndicator: () => null }));
vi.mock("./custom-status-chip", () => ({ CustomStatusChip: () => null }));
vi.mock("./nested-board/children-list", () => ({
  NestedChildrenList: ({ parent, variant }: { parent: Issue; variant: string }) => (
    <div data-testid="children-list">{`${parent.identifier} ${variant}`}</div>
  ),
}));
let nested: NestedIssue | undefined;
vi.mock("./nested-board/use-nested-children", () => ({
  useNestedIssue: (_issue: Issue, fallback?: { done: number; total: number }) => ({
    nested,
    progress: nested ? { done: nested.summary.closed, total: nested.summary.total } : fallback,
  }),
}));

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => `https://app.example${path}`,
};

const P1 = { id: "p1", identifier: "BUNN-101", title: "P1", status: "todo", priority: "none", labels: [], properties: {} } as unknown as Issue;
const child = (status: string, stage: number) => ({ id: `${status}-${stage}`, number: stage, status, stage }) as Issue;

function renderRow(expanded: boolean) {
  const children = [child("done", 1), child("cancelled", 1), child("in_review", 2), child("blocked", 2), child("backlog", 3)];
  nested = {
    summary: nestedSummary(children),
    segments: [
      { kind: "stage", current: 2, max: 3 },
      { kind: "current", status: "in_review", title: "接口联调" },
      { kind: "blocked", n: 1 },
      { kind: "awaiting_review", n: 1 },
    ],
    expanded,
    onToggle: vi.fn(),
  };
  return render(
    <I18nProvider locale="zh-Hans" resources={RESOURCES}>
      <NavigationProvider value={navigation}>
        <ListRow issue={P1} childProgress={{ done: 9, total: 9 }} />
      </NavigationProvider>
    </I18nProvider>,
  );
}

describe("ListRow on an aggregating list (A16)", () => {
  it("shows the same progress as the P1 card (A2)", () => {
    renderRow(false);
    expect(screen.getByText("2/5")).toBeInTheDocument();
    expect(screen.getByText("含 1 个已取消")).toBeInTheDocument();
    expect(screen.getByTestId("nested-progress-line").textContent).toBe(
      "阶段 2/3 · 审核中：接口联调 · 1 个阻塞 · 1 项待你验收",
    );
    expect(screen.getByRole("button", { name: "展开" })).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("children-list")).toBeNull();
  });

  it("lists the sub-issues under the row when expanded", () => {
    renderRow(true);
    expect(screen.getByRole("button", { name: "收起" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("children-list").textContent).toBe("BUNN-101 row");
  });

  it("renders as upstream outside an aggregating list", () => {
    nested = undefined;
    render(
      <I18nProvider locale="zh-Hans" resources={RESOURCES}>
        <NavigationProvider value={navigation}>
          <ListRow issue={P1} childProgress={{ done: 1, total: 2 }} />
        </NavigationProvider>
      </I18nProvider>,
    );
    expect(screen.getByText("1/2")).toBeInTheDocument();
    expect(screen.queryByTestId("nested-progress-line")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
