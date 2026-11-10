// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

const mocks = vi.hoisted(() => ({
  checkForUpdates: vi.fn(),
}));

const translations = {
  desktop: {
    updates: {
      title: "Updates",
      current_version: "Current version",
      check_section_title: "Check for updates",
      check_section_description: "Opens the Releases page",
      check_now: "Check now",
    },
  },
};

vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (selector: (resources: typeof translations) => string) =>
      selector(translations),
  }),
}));

import { UpdatesSettingsTab } from "./updates-settings-tab";

describe("UpdatesSettingsTab", () => {
  beforeEach(() => {
    mocks.checkForUpdates.mockReset();

    Object.defineProperty(window, "desktopAPI", {
      configurable: true,
      value: { appInfo: { version: "1.2.3" } },
    });
    Object.defineProperty(window, "updater", {
      configurable: true,
      value: { checkForUpdates: mocks.checkForUpdates },
    });
  });

  it("offers no automatic update switch and reports a failed Releases page open", async () => {
    mocks.checkForUpdates.mockResolvedValue({ ok: false, error: "no browser" });
    render(<UpdatesSettingsTab />);

    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Check now" }));

    expect(await screen.findByText("no browser")).toBeInTheDocument();
    expect(mocks.checkForUpdates).toHaveBeenCalledTimes(1);
  });
});
