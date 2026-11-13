// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

async function read(body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 })),
  );
  return new ApiClient("https://api.example.test").getIssueAttention();
}

const contract = {
  awaiting_reply: [{ issue_id: "g2", ancestor_ids: ["c1", "p3"] }],
  in_review: [{ issue_id: "s3", ancestor_ids: ["p1"] }],
};

it("reads a contract-shaped response as is", async () => {
  expect(await read(contract)).toEqual(contract);
});

it("defaults a missing list or ancestor chain", async () => {
  expect(await read({ in_review: [{ issue_id: "s3" }] })).toEqual({
    awaiting_reply: [],
    in_review: [{ issue_id: "s3", ancestor_ids: [] }],
  });
});

it.each([
  ["null", null],
  ["a list", []],
  ["a bad item", { awaiting_reply: [{ ancestor_ids: ["p1"] }], in_review: [] }],
  ["a bad chain", { awaiting_reply: [], in_review: [{ issue_id: "s3", ancestor_ids: "p1" }] }],
])("falls back to empty lists on %s", async (_, body) => {
  expect(await read(body)).toEqual({ awaiting_reply: [], in_review: [] });
});
