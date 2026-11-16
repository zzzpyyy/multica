import path from "node:path";
import { describe, expect, it } from "vitest";
import { isProxied, staticFile } from "../server.mjs";

describe("server.mjs (review requirement 1)", () => {
  it("forwards whole path segments only", () => {
    for (const p of ["/api", "/api/me", "/auth/send-code", "/ws", "/ws/", "/uploads/a.png"]) expect(isProxied(p)).toBe(true);
    for (const p of ["/ws-team/issues", "/apix", "/authors/1", "/uploadsx", "/", "/nest-qa/issues"]) expect(isProxied(p)).toBe(false);
  });

  it("never serves a file outside dist", () => {
    const root = path.resolve("/srv/dist");
    expect(staticFile(root, "/assets/a.js")).toBe(path.join(root, "assets/a.js"));
    expect(staticFile(root, "/../../etc/passwd")).toBe(path.join(root, "etc/passwd"));
    expect(staticFile(root, "/%2e%2e/%2e%2e/etc/passwd")).toBe(path.join(root, "etc/passwd"));
    expect(staticFile(root, "/a%00b")).toBeNull();
    expect(staticFile(root, "/%E0%A4%A")).toBeNull();
  });
});
