// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
import { describe, expect, it } from "vitest";
import type { Project } from "../types";
import {
  projectSidebarRequest,
  shownSidebarStatuses,
  builtinSidebarRequest,
  sidebarProjects,
  viewSidebarRequest,
} from "./sidebar-list";
import {
  DEFAULT_SIDEBAR_ISSUE_SETTINGS,
  DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  type SidebarIssueSettings,
  type SidebarProjectSettings,
} from "./stores/sidebar-tree-store";

const STATUSES = [
  { key: "backlog", category: "unstarted" },
  { key: "todo", category: "unstarted" },
  { key: "in_progress", category: "started" },
  { key: "merged", category: "started" },
  { key: "done", category: "done" },
  { key: "cancelled", category: "closed" },
] as const;

describe("projectSidebarRequest", () => {
  // Fork (PRD A19): every setting is a table query, aggregated by default.
  const AGG = { include_sub_issues: true, aggregate_sub_issues: {} };
  it("leaves out finished statuses and sorts by update time by default", () => {
    expect(projectSidebarRequest("p1", DEFAULT_SIDEBAR_ISSUE_SETTINGS, STATUSES, "u1")).toEqual({
      table: {
        scope: { kind: "project", project_id: "p1" },
        filters: { statuses: ["backlog", "todo", "in_progress", "merged"], ...AGG },
        sort: { field: "updated_at", direction: "desc" },
      },
    });
  });

  it("lists my agents' issues in the project through the involved relation", () => {
    const settings: SidebarIssueSettings = { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, hiddenStatuses: [], assignee: "my_agents" };
    expect(projectSidebarRequest("p1", settings, STATUSES, "u1")).toEqual({
      table: {
        scope: { kind: "my", relation: "involved" },
        filters: { project_ids: ["p1"], ...AGG },
        sort: { field: "updated_at", direction: "desc" },
      },
    });
  });

  it("lists my own issues through the assigned relation", () => {
    const settings: SidebarIssueSettings = { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, hiddenStatuses: [], assignee: "me" };
    expect(projectSidebarRequest("p1", settings, STATUSES, "u1")?.table).toMatchObject({
      scope: { kind: "project", project_id: "p1" },
      filters: { relations: ["assigned"], ...AGG },
    });
    expect(projectSidebarRequest("p1", settings, STATUSES, undefined)).toBeNull();
  });

  it("lists every issue when aggregation is off", () => {
    const settings: SidebarIssueSettings = { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, hiddenStatuses: [], aggregateSubIssues: false };
    expect(projectSidebarRequest("p1", settings, STATUSES, "u1")?.table.filters).toEqual({ include_sub_issues: true });
  });

  it("lists me and my agents through the table query, which can OR the two", () => {
    const settings: SidebarIssueSettings = { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, assignee: "me_and_my_agents" };
    expect(projectSidebarRequest("p1", settings, STATUSES, "u1")).toEqual({
      table: {
        scope: { kind: "project", project_id: "p1" },
        filters: {
          statuses: ["backlog", "todo", "in_progress", "merged"],
          relations: ["assigned", "my_agents"],
          ...AGG,
        },
        sort: { field: "updated_at", direction: "desc" },
      },
    });
    expect(projectSidebarRequest("p1", settings, STATUSES, undefined)).toBeNull();
  });

  it("lists nothing when every status is hidden", () => {
    const settings = { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, hiddenStatuses: STATUSES.map((s) => s.key) };
    expect(shownSidebarStatuses(settings, STATUSES)).toEqual([]);
    expect(projectSidebarRequest("p1", settings, STATUSES, "u1")).toBeNull();
  });
});

describe("viewSidebarRequest", () => {
  const TODAY = "2026-03-11";
  const workspace = { display: {}, scope_type: "workspace" as const, scope_variant: null };

  it("carries the view's own filters, variant and ordering", () => {
    expect(
      viewSidebarRequest(
        {
          query: { statusFilters: ["todo"], labelFilters: ["l1"], priorityFilters: ["bogus"] },
          display: { sortBy: "due_date", sortDirection: "desc", showSubIssues: false, viewMode: "table" },
          scope_variant: "agents",
        },
        TODAY,
      ),
    ).toEqual({
      table: {
        scope: { kind: "workspace", assignee_types: ["agent", "squad"] },
        filters: { statuses: ["todo"], label_ids: ["l1"], include_sub_issues: false },
        sort: { field: "due_date", direction: "desc" },
      },
    });
  });

  it("keeps the conditions the plain list endpoint could not express", () => {
    const filters = (query: Record<string, unknown>) => viewSidebarRequest({ ...workspace, query }, TODAY)?.table.filters;
    expect(filters({ relationFilters: ["assigned"] })).toMatchObject({ relations: ["assigned"] });
    expect(filters({ statusFilters: ["done"], negatedFilters: ["status"] })).toMatchObject({
      statuses: ["done"],
      negate: ["statuses"],
    });
    expect(filters({ dueDateFilters: ["overdue"] })).toMatchObject({ due_dates: ["overdue"], today: TODAY });
    expect(filters({ projectStatusFilters: ["completed"] })).toMatchObject({ project_statuses: ["completed"] });
    expect(
      filters({
        advancedFilter: {
          type: "group",
          id: "root",
          match: "any",
          children: [{ type: "condition", id: "c1", field: "priority", negated: false, values: ["urgent"] }],
        },
      }),
    ).toMatchObject({ advanced: { match: "any", children: [{ field: "priorities", values: ["urgent"] }] } });
    expect(filters({ agentRunningFilter: true })).toMatchObject({ working_only: true });
    expect(filters({ unreadFilter: true })).toMatchObject({ unread_issue_ids: [] });
    expect(
      viewSidebarRequest({ ...workspace, query: { unreadFilter: true } }, TODAY, ["b", "a"])?.table.filters,
    ).toMatchObject({ unread_issue_ids: ["a", "b"] });
    expect(
      viewSidebarRequest({ ...workspace, query: {} }, TODAY, ["a"])?.table.filters,
    ).not.toHaveProperty("unread_issue_ids");
    // "Is not" with nothing selected is not a condition, and no due-date rule needs no day.
    expect(filters({ negatedFilters: ["status"] })).toEqual({ include_sub_issues: true, aggregate_sub_issues: {} });
  });

  it("scopes project and my views like the page they open", () => {
    const base = { query: {}, display: {} };
    const scope = (view: Parameters<typeof viewSidebarRequest>[0]) => viewSidebarRequest(view, TODAY)?.table.scope;
    expect(scope({ ...base, scope_type: "project", scope_id: "p1", scope_variant: null })).toEqual({
      kind: "project",
      project_id: "p1",
    });
    expect(scope({ ...base, scope_type: "my", scope_id: null, scope_variant: "created" })).toEqual({
      kind: "my",
      relation: "created",
    });
    expect(scope({ ...base, scope_type: "my", scope_id: null, scope_variant: null })).toEqual({
      kind: "my",
      relation: "any",
    });
    expect(scope({ ...base, scope_type: "project", scope_id: null, scope_variant: null })).toBeUndefined();
  });

  it("drops the dimension the scope already fixes", () => {
    const project = viewSidebarRequest(
      { query: { projectFilters: ["p2"], negatedFilters: ["project"] }, display: {}, scope_type: "project", scope_id: "p1", scope_variant: null },
      TODAY,
    );
    expect(project?.table.filters).toEqual({ include_sub_issues: true, aggregate_sub_issues: {} });
    const mine = viewSidebarRequest(
      { query: { relationFilters: ["created"] }, display: {}, scope_type: "my", scope_id: null, scope_variant: "assigned" },
      TODAY,
    );
    expect(mine?.table.filters).toEqual({ include_sub_issues: true, aggregate_sub_issues: {} });
  });

  // Fork (PRD A19, r3 §1.4): the sidebar aggregates a view when opening it does.
  it("aggregates like the view opens: local layout first, then display", () => {
    const view = { ...workspace, query: {} };
    const sub = (display: Record<string, unknown>, local?: Parameters<typeof viewSidebarRequest>[3]) => {
      const f = viewSidebarRequest({ ...view, display }, TODAY, [], local)?.table.filters;
      return { include: f?.include_sub_issues, aggregate: f?.aggregate_sub_issues };
    };
    const on = { include: true, aggregate: {} };
    expect(sub({})).toEqual(on);
    expect(sub({ viewMode: "list" })).toEqual(on);
    expect(sub({ viewMode: "table", showSubIssues: false })).toEqual({ include: false, aggregate: undefined });
    // Switched to a table on this device: the view opens unaggregated.
    expect(sub({ viewMode: "board" }, { viewMode: "table" })).toEqual({ include: true, aggregate: undefined });
    expect(sub({ viewMode: "table" }, { viewMode: "board" })).toEqual(on);
    expect(sub({ viewMode: "board" }, { viewMode: "board", nestedSubIssues: false })).toEqual({ include: true, aggregate: undefined });
  });
});

describe("builtinSidebarRequest", () => {
  it("scopes each built-in like its page and lists with the sidebar settings", () => {
    const request = (builtin: Parameters<typeof builtinSidebarRequest>[0], relation: Parameters<typeof builtinSidebarRequest>[1] = "assigned") =>
      builtinSidebarRequest(builtin, relation, DEFAULT_SIDEBAR_ISSUE_SETTINGS, STATUSES)?.table;
    expect(request("all_issues")).toEqual({
      scope: { kind: "workspace" },
      filters: { statuses: ["backlog", "todo", "in_progress", "merged"], include_sub_issues: true, aggregate_sub_issues: {} },
      sort: { field: "updated_at", direction: "desc" },
    });
    expect(request("my_issues", "created")?.scope).toEqual({ kind: "my", relation: "created" });
    expect(request("my_issues", "all")?.scope).toEqual({ kind: "my", relation: "any" });
    expect(request("my_agents", "assigned")?.scope).toEqual({ kind: "my", relation: "involved" });
  });

  it("maps the assignee setting and lists nothing when every status is hidden", () => {
    const mine = { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, hiddenStatuses: [], assignee: "my_agents" as const };
    expect(builtinSidebarRequest("all_issues", "assigned", mine, STATUSES)?.table.filters).toEqual({
      relations: ["my_agents"],
      include_sub_issues: true,
      aggregate_sub_issues: {},
    });
    const both = { ...mine, assignee: "me_and_my_agents" as const };
    expect(builtinSidebarRequest("all_issues", "assigned", both, STATUSES)?.table.filters).toEqual({
      relations: ["assigned", "my_agents"],
      include_sub_issues: true,
      aggregate_sub_issues: {},
    });
    const off = { ...mine, aggregateSubIssues: false };
    expect(builtinSidebarRequest("all_issues", "assigned", off, STATUSES)?.table.filters).toEqual({
      relations: ["my_agents"],
      include_sub_issues: true,
    });
    const none = { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, hiddenStatuses: STATUSES.map((s) => s.key) };
    expect(builtinSidebarRequest("all_issues", "assigned", none, STATUSES)).toBeNull();
  });
});

describe("sidebarProjects", () => {
  const project = (id: string, title: string, status: Project["status"], lead_id: string | null = null) =>
    ({
      id,
      title,
      status,
      lead_type: lead_id ? "member" : null,
      lead_id,
      created_at: `2026-10-0${id.slice(1)}T00:00:00Z`,
      updated_at: `2026-10-0${id.slice(1)}T00:00:00Z`,
    }) as Project;
  const projects = [
    project("p1", "Beta", "in_progress", "u1"),
    project("p2", "Alpha", "planned"),
    project("p3", "Gamma", "completed"),
    project("p4", "Delta", "paused"),
  ];

  it("follows the manual order, then list order for projects it does not name", () => {
    expect(sidebarProjects(projects, DEFAULT_SIDEBAR_PROJECT_SETTINGS, ["p4"], "u1").map((p) => p.id)).toEqual([
      "p4",
      "p1",
      "p2",
    ]);
  });

  it("filters by status and lead, and sorts by name", () => {
    const settings: SidebarProjectSettings = {
      ...DEFAULT_SIDEBAR_PROJECT_SETTINGS,
      ordering: "name",
      statuses: ["completed", "planned"],
    };
    expect(sidebarProjects(projects, settings, [], "u1").map((p) => p.title)).toEqual(["Alpha", "Gamma"]);
    expect(
      sidebarProjects(projects, { ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, onlyMine: true }, [], "u1").map((p) => p.id),
    ).toEqual(["p1"]);
  });
});
