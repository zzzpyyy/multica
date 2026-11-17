import { describe, expect, it } from "vitest";
import { pageState, type PageQueries } from "./queries";

// BUNN-31 N10 (一鸣 11-17): the issue columns wait for attention, the
// snapshot and the names; a failed attention or snapshot is a failure, never
// "nobody waits for you". The race itself is checked in a browser with the
// responses held back (BUNN-31).
const ok = { data: [], isError: false };
const pending = { data: undefined, isError: false };
const failed = { data: undefined, isError: true };
const page = (over: Partial<PageQueries> = {}): PageQueries => ({
  issues: ok, statuses: ok, attention: ok, snapshot: ok, names: [ok, ok, ok], ...over,
});

describe("pageState", () => {
  it("is ready only when everything grouping and 在等谁 read is in", () => {
    expect(pageState(page())).toEqual({ kind: "ready" });
    for (const k of ["issues", "statuses", "attention", "snapshot"] as const) {
      expect(pageState(page({ [k]: pending }))).toEqual({ kind: "loading" });
    }
    expect(pageState(page({ names: [ok, pending, ok] }))).toEqual({ kind: "loading" });
  });

  it("never groups without attention or the snapshot: their failure is shown", () => {
    expect(pageState(page({ attention: failed }))).toEqual({ kind: "failed", what: ["等你处理的信息"] });
    expect(pageState(page({ snapshot: failed, issues: failed }))).toEqual({ kind: "failed", what: ["任务", "智能体的运行情况"] });
  });

  it("statuses and names degrade as before; a later refetch error keeps the data", () => {
    expect(pageState(page({ statuses: failed, names: [failed, ok, failed] }))).toEqual({ kind: "ready" });
    expect(pageState(page({ attention: { data: [], isError: true } }))).toEqual({ kind: "ready" });
  });
});
