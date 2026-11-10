// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

async function read(body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 })),
  );
  return new ApiClient("https://api.example.test").listIssueInstances("issue-1");
}

const batch = {
  id: "b1",
  issue_id: "issue-1",
  agent: { id: "a1", name: "TB" },
  total: 1,
  priority: false,
  requested_by: { type: "member", id: "m1", name: "QA" },
  created_at: "2026-11-10T00:00:00Z",
  settled_at: null,
  instances: [
    {
      index: 1,
      split: "a",
      status: "queued",
      task_id: "t1",
      task_ids: ["t1"],
      session_id: null,
      work_dir: null,
      branch_name: null,
      started_at: null,
      completed_at: null,
      queue: { position: 1, reason: "role_full", running: 3, limit: 3 },
    },
  ],
};

it("reads a contract-shaped response as is", async () => {
  expect(await read({ batches: [batch] })).toEqual({ batches: [batch] });
});

it("defaults nullable instance fields an older server omits", async () => {
  const { batches } = await read({
    batches: [{ ...batch, instances: [{ index: 1, split: "a", status: "running", task_id: "t1" }] }],
  });
  expect(batches[0]!.instances[0]).toMatchObject({ task_ids: [], session_id: null, branch_name: null, queue: null });
});

it("falls back to no batches for a malformed body", async () => {
  expect(await read({ batches: [{ id: 1 }] })).toEqual({ batches: [] });
  expect(await read([])).toEqual({ batches: [] });
});
