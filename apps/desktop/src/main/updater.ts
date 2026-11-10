// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
import { autoUpdater, type UpdateDownloadedEvent } from "electron-updater";
import { app, type BrowserWindow, type MessageBoxOptions, ipcMain } from "electron";
import type { ManualUpdateCheckResult } from "../shared/updater-types";
import { FORK_RELEASES_URL } from "../shared/fork";
import { openExternalSafely } from "./external-url";
import type { DaemonInstallPrep } from "./update-install-gate";

// Silent background updates: electron-updater downloads on its own as soon
// as `update-available` fires; we only surface UI when the package is fully
// downloaded and ready to install on next quit.
autoUpdater.autoDownload = true;
autoUpdater.autoInstallOnAppQuit = true;

// Windows arm64 ships its own update metadata channel because
// electron-builder's `latest.yml` is not arch-suffixed on Windows — both
// arches would otherwise collide on the same file in the GitHub Release.
// See scripts/package.mjs (builderArgsForTarget) for the publish-side half
// of this pact. Pin the channel here so arm64 clients fetch
// `latest-arm64.yml` instead of the x64 metadata.
if (process.platform === "win32" && process.arch === "arm64") {
  autoUpdater.channel = "latest-arm64";
}

interface ChannelConfigurableUpdater {
  channel: string | null;
  allowDowngrade: boolean;
}

export function configureMacX64UpdateChannel(
  updater: ChannelConfigurableUpdater,
  platform: NodeJS.Platform = process.platform,
  arch: string = process.arch,
): void {
  if (platform !== "darwin" || arch !== "x64") return;

  // AppUpdater.channel enables allowDowngrade as a side effect. This channel
  // isolates a CPU architecture, not a release train, so preserve normal
  // monotonic version behavior after selecting the architecture feed.
  updater.channel = "latest-x64";
  updater.allowDowngrade = false;
}

// electron-builder does not architecture-suffix macOS update metadata.
// package.mjs publishes macOS x64 as `latest-x64-mac.yml`; the established
// arm64 feed and runtime path remain unchanged.
configureMacX64UpdateChannel(autoUpdater);

// quitAndInstall only quits once the installer has been spawned. If the app is
// still running this long afterwards, the install bailed and the daemon we
// stopped for it must be brought back.
const INSTALL_QUIT_WATCHDOG_MS = 10_000;

// Injected so the updater does not own the daemon manager's state.
export interface UpdaterDaemonGate {
  prepare: (options: {
    interruptActiveTasks: boolean;
  }) => Promise<DaemonInstallPrep>;
  resumeAfterAbortedInstall: () => void;
  confirmInstallWhileBusy: (activeTasks: number) => Promise<boolean>;
}

export interface SetupAutoUpdaterOptions {
  daemonGate?: UpdaterDaemonGate;
  platform?: NodeJS.Platform;
}

export function createElectronBusyInstallPrompt(
  showMessageBox: (
    options: MessageBoxOptions,
  ) => Promise<{ response: number }>,
): (activeTasks: number) => Promise<boolean> {
  return async (activeTasks) => {
    const tasks = activeTasks === 1 ? "1 agent task is" : `${activeTasks} agent tasks are`;
    const { response } = await showMessageBox({
      type: "question",
      buttons: ["Install now", "Cancel"],
      defaultId: 1,
      cancelId: 1,
      title: "Install update",
      message: `${tasks} running on this computer.`,
      detail: "Installing now stops the local runtime and interrupts these tasks.",
    });
    return response === 0;
  };
}

type RendererChannel =
  | "updater:update-available"
  | "updater:download-progress"
  | "updater:update-downloaded";

function isDestroyedObjectError(err: unknown): boolean {
  return err instanceof Error && err.message.includes("Object has been destroyed");
}

function sendToLiveRenderer(
  win: BrowserWindow | null,
  channel: RendererChannel,
  payload: unknown,
): void {
  if (!win || win.isDestroyed()) return;

  try {
    const { webContents } = win;
    if (webContents.isDestroyed()) return;
    webContents.send(channel, payload);
  } catch (err) {
    if (isDestroyedObjectError(err)) return;
    throw err;
  }
}

export function setupAutoUpdater(
  getMainWindows: () => readonly BrowserWindow[],
  options: SetupAutoUpdaterOptions = {},
): void {
  // Every main window mounts the update banner, so each one hears progress.
  const broadcast = (channel: RendererChannel, payload: unknown) => {
    for (const win of getMainWindows()) sendToLiveRenderer(win, channel, payload);
  };
  const platform = options.platform ?? process.platform;
  // Only Windows locks a running executable against replacement; macOS and
  // Linux keep their existing update path untouched.
  const daemonGate = platform === "win32" ? (options.daemonGate ?? null) : null;
  let updateReadyToInstall = false;
  let installFlow: Promise<void> | null = null;
  let quitRequested = false;
  let finishing = false;
  let appQuitting = false;

  const finish = (install: boolean, manual: boolean): void => {
    finishing = true;
    const previousAutoInstall = autoUpdater.autoInstallOnAppQuit;
    if (!install) autoUpdater.autoInstallOnAppQuit = false;
    setTimeout(() => {
      if (appQuitting) return;
      finishing = false;
      quitRequested = false;
      autoUpdater.autoInstallOnAppQuit = previousAutoInstall;
      daemonGate?.resumeAfterAbortedInstall();
    }, INSTALL_QUIT_WATCHDOG_MS);
    if (install && manual) autoUpdater.quitAndInstall(false, true);
    else app.quit();
  };

  // All entry points join this promise. Once stopping starts, nobody cancels
  // or replaces its PID wait, and no other flow can resume the daemon.
  const installWithDaemonGate = (manual: boolean): Promise<void> => {
    if (installFlow) return installFlow;
    if (finishing || !daemonGate) return Promise.resolve();
    installFlow = (async () => {
      let install = true;
      try {
        const prep = await daemonGate.prepare({ interruptActiveTasks: false });
        if (prep.kind === "busy") {
          install = manual && await daemonGate.confirmInstallWhileBusy(prep.activeTasks);
          if (install) await daemonGate.prepare({ interruptActiveTasks: true });
        }
      } catch (err) {
        // Preserve the existing installer fallback if preparation fails.
        console.error("[updater] daemon preparation failed; installing anyway:", err);
      }
      if (install) finish(true, manual);
      else {
        daemonGate.resumeAfterAbortedInstall();
        if (quitRequested) finish(false, false);
      }
    })().finally(() => { installFlow = null; });
    return installFlow;
  };

  if (daemonGate) {
    app.on("will-quit", () => { appQuitting = true; });
    app.on("before-quit", (event) => {
      if (finishing || !updateReadyToInstall || !autoUpdater.autoInstallOnAppQuit) return;
      event.preventDefault();
      quitRequested = true;
      void installWithDaemonGate(false).catch((err) => {
        console.error("[updater] quit-time installation failed:", err);
      });
    });
  }

  autoUpdater.on("update-available", (info) => {
    // Forwarded for renderer-side state tracking only; the notification UI
    // does not render an "available" affordance with autoDownload=true.
    broadcast("updater:update-available", {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on("download-progress", (progress) => {
    broadcast("updater:download-progress", {
      percent: progress.percent,
    });
  });

  autoUpdater.on("update-downloaded", (info: UpdateDownloadedEvent) => {
    updateReadyToInstall = true;
    broadcast("updater:update-downloaded", {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on("error", (err) => {
    console.error("Auto-updater error:", err);
  });

  // Retained for IPC back-compat with older renderer bundles. With
  // autoDownload=true the renderer no longer triggers this path.
  ipcMain.handle("updater:download", () => {
    return autoUpdater.downloadUpdate();
  });

  ipcMain.handle("updater:install", async () => {
    if (!daemonGate) {
      autoUpdater.quitAndInstall(false, true);
      return;
    }
    await installWithDaemonGate(true);
  });

  // Fork builds are unsigned and cannot update in place, so nothing calls
  // checkForUpdates; a signed-build flag would restore those checks here.
  ipcMain.handle("updater:check", async (): Promise<ManualUpdateCheckResult> => {
    try {
      await openExternalSafely(FORK_RELEASES_URL);
      return { ok: true };
    } catch (err) {
      return {
        ok: false,
        error: err instanceof Error ? err.message : String(err),
      };
    }
  });
}
