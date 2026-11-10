import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { issueKeys } from "@multica/core/issues/queries";
import type { ListInstancesResponse, TaskInstance } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { InstanceLabel, InstancesSection, findTaskInstance } from "./instances-section";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

function instance(overrides: Partial<TaskInstance>): TaskInstance {
  return {
    index: 1,
    split: "a",
    status: "running",
    task_id: "t1",
    task_ids: ["t1"],
    session_id: null,
    work_dir: null,
    branch_name: null,
    started_at: null,
    completed_at: null,
    queue: null,
    ...overrides,
  };
}

const queued = (index: number, reason: string, running: number | null = null, limit: number | null = null) =>
  instance({
    index,
    split: `split-${reason}`,
    status: "queued",
    task_id: `q${index}`,
    task_ids: [`q${index}`],
    queue: { position: index, reason: reason as never, running, limit },
  });

const response: ListInstancesResponse = {
  batches: [
    {
      id: "b2",
      issue_id: "issue-1",
      agent: { id: "a1", name: "TB" },
      total: 4,
      priority: true,
      requested_by: { type: "member", id: "m1", name: "QA" },
      created_at: "2026-11-10T00:00:00Z",
      settled_at: null,
      instances: [
        queued(1, "role_full", 3, 3),
        queued(2, "runtime_full", 1, 1),
        queued(3, "runtime_offline"),
        queued(4, "waiting_dispatch"),
      ],
    },
    {
      id: "b1",
      issue_id: "issue-1",
      agent: { id: "a1", name: "TB" },
      total: 3,
      priority: false,
      requested_by: { type: "member", id: "m1", name: "QA" },
      created_at: "2026-11-09T00:00:00Z",
      settled_at: null,
      instances: [
        instance({ index: 1, split: "a", task_id: "r1", task_ids: ["r1"], branch_name: "agent/tb/abc123", session_id: "sess-1" }),
        // Retried once: the first attempt's comments still belong to instance 2.
        instance({ index: 2, split: "b", status: "completed", task_id: "r2b", task_ids: ["r2a", "r2b"] }),
        instance({ index: 3, split: "c", status: "cancelled", task_id: "r3", task_ids: ["r3"] }),
      ],
    },
  ],
};

function renderWith(ui: ReactNode, data: ListInstancesResponse, locale: "en" | "zh-Hans" = "zh-Hans") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(issueKeys.instances("ws-1", "issue-1"), data);
  return renderWithI18n(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>, { locale });
}

afterEach(cleanup);

describe("findTaskInstance", () => {
  it("maps every attempt of an instance to its index, batch total and split", () => {
    expect(findTaskInstance(response, "r2a")).toEqual({ index: 2, total: 3, split: "b" });
    expect(findTaskInstance(response, "r2b")).toEqual({ index: 2, total: 3, split: "b" });
    expect(findTaskInstance(response, "q4")).toEqual({ index: 4, total: 4, split: "split-waiting_dispatch" });
  });

  it("returns nothing for a run that is not an instance", () => {
    expect(findTaskInstance(response, "ordinary-run")).toBeUndefined();
    expect(findTaskInstance({ batches: [] }, "r1")).toBeUndefined();
  });
});

describe("InstancesSection", () => {
  it("shows each queue reason in the PRD's words with the queue position", () => {
    renderWith(<InstancesSection issueId="issue-1" />, response);
    expect(screen.getByText("第 1 位 · 角色名额已满（3/3）")).toBeTruthy();
    expect(screen.getByText("第 2 位 · 运行环境已满")).toBeTruthy();
    expect(screen.getByText("第 3 位 · 运行环境离线")).toBeTruthy();
    expect(screen.getByText("第 4 位 · 等待领取")).toBeTruthy();
  });

  it("lists index, split and status per instance, newest batch first", () => {
    renderWith(<InstancesSection issueId="issue-1" />, response, "en");
    const section = screen.getByTestId("instances-section");
    expect(section.textContent).toMatch(/Priority.*Instance 1\/4.*split-role_full.*Instance 1\/3/);
    expect(screen.getAllByText("Queued")).toHaveLength(4);
    expect(screen.getByText("Working")).toBeTruthy();
    expect(screen.getByText("Completed")).toBeTruthy();
    expect(screen.getByText("Cancelled")).toBeTruthy();
    expect(screen.getByText("No. 1 in queue · Role at capacity (3/3)")).toBeTruthy();
    expect(screen.getByText("agent/tb/abc123")).toBeTruthy();
    expect(screen.getByText("Session sess-1")).toBeTruthy();
    // Read-only: no add or cancel controls.
    expect(section.querySelector("button, input, textarea")).toBeNull();
  });

  it("renders nothing when the issue has no instances", () => {
    renderWith(<InstancesSection issueId="issue-1" />, { batches: [] });
    expect(screen.queryByTestId("instances-section")).toBeNull();
  });
});

describe("InstanceLabel", () => {
  it("labels a comment posted by an earlier attempt of an instance", () => {
    renderWith(<InstanceLabel issueId="issue-1" taskId="r2a" />, response);
    expect(screen.getByText("实例 2/3 · b")).toBeTruthy();
  });

  it("renders nothing for an ordinary run's comment", () => {
    const { container } = renderWith(<InstanceLabel issueId="issue-1" taskId="ordinary-run" />, response);
    expect(container.textContent).toBe("");
  });
});
