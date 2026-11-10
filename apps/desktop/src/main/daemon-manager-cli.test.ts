// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

const ctx = vi.hoisted(() => ({
  handlers: new Map<string, (...args: unknown[]) => unknown>(),
  exec: vi.fn(),
  exists: vi.fn(),
  install: vi.fn(),
  versions: {} as Record<string, string>,
}));
vi.mock("electron", () => ({
  app: { getAppPath: () => "/test/desktop", getPreferredSystemLanguages: () => ["en"], on: vi.fn() },
  BrowserWindow: { getAllWindows: () => [] },
  ipcMain: {
    handle: (name: string, handler: (...args: unknown[]) => unknown) => { ctx.handlers.set(name, handler); },
    on: vi.fn(),
  },
}));
vi.mock("child_process", () => ({ execFile: ctx.exec }));
vi.mock("fs", async (original) => ({ ...await original<typeof import("fs")>(), existsSync: ctx.exists }));
vi.mock("fs/promises", async (original) => ({
  ...await original<typeof import("fs/promises")>(),
  readFile: vi.fn(async () => JSON.stringify({ autoStart: true, autoStop: false })),
}));
vi.mock("./cli-bootstrap", () => ({ ensureManagedCli: ctx.install, managedCliPath: () => "/test/managed/multica" }));

const BUNDLED = "/test/desktop/resources/bin/multica";
const MANAGED = "/test/managed/multica";

async function setup() {
  const { setupDaemonManager } = await import("./daemon-manager");
  setupDaemonManager(() => null);
  return (name: string) => ctx.handlers.get(name)!({});
}

describe("desktop uses only fork builds of the CLI", () => {
  beforeEach(() => {
    vi.resetModules(); vi.clearAllMocks(); ctx.handlers.clear();
    ctx.exists.mockImplementation((path: string) => path !== BUNDLED);
    ctx.install.mockRejectedValue(new Error("download blocked"));
    // Every binary not listed (the managed copy, the PATH hit) is upstream 0.8.1.
    ctx.versions = {};
    ctx.exec.mockImplementation((bin: string, args: string[], _options: unknown, callback: (error: Error | null, output?: string) => void) => {
      if (args[0] === "version") callback(null, JSON.stringify({ version: ctx.versions[bin] ?? "0.8.1" }));
      else callback(null);
    });
    vi.spyOn(console, "log").mockImplementation(() => {});
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  it("skips an upstream CLI from every source, reports each one, and never starts the daemon", async () => {
    const ipc = await setup();

    await expect(ipc("daemon:is-cli-installed")).resolves.toBe(false);
    await ipc("daemon:auto-start");

    await expect(ipc("daemon:get-status")).resolves.toEqual({
      state: "cli_not_found",
      cliSources: [
        { source: "bundled", result: "missing" },
        { source: "managed", result: "not_fork", version: "0.8.1" },
        { source: "download", result: "failed" },
        { source: "path", result: "not_fork", version: "0.8.1" },
      ],
    });
    expect(ctx.install).toHaveBeenCalledWith({ forceInstall: true });
    expect(ctx.exec.mock.calls.some(([, args]) => args[0] === "daemon")).toBe(false);
  });

  it("uses the first fork build after rejecting an upstream one", async () => {
    ctx.exists.mockReturnValue(true);
    ctx.versions = { [MANAGED]: "0.8.1-fork.1" };
    const ipc = await setup();

    await expect(ipc("daemon:is-cli-installed")).resolves.toBe(true);

    expect(ctx.exec.mock.calls.map(([bin]) => bin)).toEqual([BUNDLED, MANAGED]);
    expect(ctx.install).not.toHaveBeenCalled();
  });
});
