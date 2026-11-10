// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
import { useCallback, useState } from "react";
import { AlertCircle } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "@multica/views/i18n";
import { SettingsCard, SettingsRow, SettingsTab } from "@multica/views/settings";

export function UpdatesSettingsTab() {
  const { t } = useT("settings");
  const [error, setError] = useState<string | null>(null);
  const currentVersion = window.desktopAPI.appInfo.version;

  // Opens the fork's Releases page; new versions are installed by hand.
  const handleCheck = useCallback(async () => {
    const result = await window.updater.checkForUpdates();
    setError(result.ok ? null : result.error);
  }, []);

  return (
    <SettingsTab
      title={t(($) => $.desktop.updates.title)}
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.desktop.updates.current_version)}>
          <span className="font-mono text-caption text-muted-foreground">
            v{currentVersion}
          </span>
        </SettingsRow>

        <SettingsRow
          label={t(($) => $.desktop.updates.check_section_title)}
          align="start"
          description={
            <>
              <p>{t(($) => $.desktop.updates.check_section_description)}</p>
              {error && (
                <p className="mt-2 inline-flex items-center gap-1.5 text-destructive">
                  <AlertCircle className="size-3.5" />
                  {error}
                </p>
              )}
            </>
          }
        >
          <Button variant="outline" size="sm" onClick={handleCheck}>
            {t(($) => $.desktop.updates.check_now)}
          </Button>
        </SettingsRow>
      </SettingsCard>
    </SettingsTab>
  );
}
