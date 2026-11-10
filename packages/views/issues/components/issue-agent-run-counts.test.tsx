import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotKeys } from "@multica/core/agents/queries";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueAgentRunCounts, issueAgentRunCounts } from "./issue-agent-run-counts";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => (id === "tb" ? "TB" : id) }),
}));

const task = (id: string, issue_id: string, agent_id: string, status: AgentTask["status"]) =>
  ({ id, issue_id, agent_id, status }) as AgentTask;

// D5: X has 2 running + 1 queued TB instances while Y holds TB's third slot.
const snapshot = [
  task("x1", "X", "tb", "running"),
  task("x2", "X", "tb", "dispatched"),
  task("x3", "X", "tb", "queued"),
  task("x4", "X", "tl", "queued"),
  task("x5", "X", "tb", "completed"),
  task("y1", "Y", "tb", "running"),
];

afterEach(cleanup);

describe("issueAgentRunCounts", () => {
  it("counts one issue's active runs per agent; a claimed run already holds its slot", () => {
    expect(issueAgentRunCounts(snapshot, "X")).toEqual([
      { agentId: "tb", running: 2, queued: 1 },
      { agentId: "tl", running: 0, queued: 1 },
    ]);
  });

  it("is empty for an issue with only finished runs", () => {
    expect(issueAgentRunCounts([task("d", "Z", "tb", "completed")], "Z")).toEqual([]);
  });
});

describe("IssueAgentRunCounts", () => {
  function renderFor(issueId: string) {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    qc.setQueryData(agentTaskSnapshotKeys.list("ws-1"), snapshot);
    return renderWithI18n(
      <QueryClientProvider client={qc}><IssueAgentRunCounts issueId={issueId} /></QueryClientProvider>,
      { locale: "zh-Hans" },
    );
  }

  it("shows the board-card line per agent", () => {
    renderFor("X");
    expect(screen.getByText("TB 运行 2 · 排队 1")).toBeTruthy();
    expect(screen.getByText("tl 运行 0 · 排队 1")).toBeTruthy();
  });

  it("renders nothing for an idle issue", () => {
    renderFor("idle");
    expect(screen.queryByTestId("agent-run-counts")).toBeNull();
  });
});
