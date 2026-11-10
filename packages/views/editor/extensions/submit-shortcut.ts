// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
import { Extension } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import {
  createShortcutChord,
  getShortcut,
  isPlainShortcut,
  shortcutMatchesEvent,
  type ShortcutChord,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";

/**
 * - `message`: inputs that send one message (comments, replies, chat). Follows
 *   the `send` setting; when that is plain Enter, ⌘/Ctrl+Enter sends too.
 * - `longText`: inputs written in several paragraphs before submitting (new
 *   issue description, feedback, squad notes…). Always ⌘/Ctrl+Enter whatever
 *   `send` says, so the first Enter of a draft never creates an issue — or
 *   starts an agent run — early.
 */
export type SubmitShortcutMode = "message" | "longText";

/** The fixed submit chord of long-text inputs: ⌘+Enter, Ctrl+Enter off macOS. */
export const LONG_TEXT_SUBMIT_SHORTCUT = createShortcutChord("Enter", { primary: true });

function submitShortcutsFor(
  mode: SubmitShortcutMode,
  configuredShortcut: ShortcutChord | null,
): ShortcutChord[] {
  if (mode === "longText") return [LONG_TEXT_SUBMIT_SHORTCUT];
  if (!configuredShortcut) return [];
  return isPlainShortcut(configuredShortcut, "Enter")
    ? [configuredShortcut, LONG_TEXT_SUBMIT_SHORTCUT]
    : [configuredShortcut];
}

/**
 * What a keydown means for the host's submit action:
 * - `"submit"`: run it;
 * - `"swallow"`: the submit chord is being held — consume the auto-repeat so it
 *   neither submits again nor falls through to the editor's Enter handling
 *   (which would insert empty paragraphs into the just-cleared composer);
 * - `null`: not a submit key, or the Enter that commits an IME composition.
 */
export function submitKeyAction(
  event: KeyboardEvent,
  mode: SubmitShortcutMode,
  options: { configuredShortcut?: ShortcutChord | null; composing?: boolean } = {},
): "submit" | "swallow" | null {
  if (options.composing || isImeComposing(event)) return null;
  const shortcuts = submitShortcutsFor(mode, options.configuredShortcut ?? null);
  if (!shortcuts.some((shortcut) => shortcutMatchesEvent(shortcut, event))) return null;
  return event.repeat ? "swallow" : "submit";
}

/**
 * When plain Enter becomes Send, Shift+Enter must inherit the old Enter
 * behavior instead of Tiptap's normal hard-break behavior. Replaying the
 * editor's Enter keymap preserves paragraphs, patched list exit/continuation,
 * code-block newlines, and open suggestion pickers.
 */
export function shouldReplayNativeEnter(
  event: KeyboardEvent,
  configuredShortcut: ShortcutChord | null,
  composing: boolean,
): boolean {
  return (
    !composing &&
    !isImeComposing(event) &&
    isPlainShortcut(configuredShortcut, "Enter") &&
    event.key === "Enter" &&
    event.shiftKey &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.altKey
  );
}

/**
 * Tiered-priority dynamic key handler. Suggestion and slash-command plugins
 * get first refusal, while submit still runs before the base editor keymap.
 *
 * `canSubmit` reports whether the host has a submit action at all. Without
 * one, Enter keeps its editing meaning — including held-key repeats and
 * Shift+Enter — as in an editor that never had a submit shortcut.
 */
export function createSubmitShortcutExtension(
  onSubmit: () => boolean,
  {
    mode = "message",
    canSubmit = () => true,
  }: { mode?: SubmitShortcutMode; canSubmit?: () => boolean } = {},
) {
  return Extension.create({
    name: "submitShortcut",
    // Mention/slash extensions use 101, so open pickers get first refusal.
    // Default editor keymaps use 100; reverse extension order places this
    // late-added handler ahead of them while keeping picker priority intact.
    priority: 100,
    addProseMirrorPlugins() {
      const editor = this.editor;
      let replayingEnter = false;
      return [
        new Plugin({
          props: {
            handleKeyDown(view, event) {
              if (replayingEnter || !canSubmit()) return false;
              const shortcut = getShortcut("send");

              if (
                mode === "message" &&
                shouldReplayNativeEnter(event, shortcut, view.composing)
              ) {
                replayingEnter = true;
                try {
                  return editor.commands.keyboardShortcut("Enter");
                } finally {
                  replayingEnter = false;
                }
              }

              const action = submitKeyAction(event, mode, {
                configuredShortcut: shortcut,
                composing: view.composing,
              });
              if (action === "swallow") return true;
              if (action !== "submit") return false;
              return onSubmit();
            },
          },
        }),
      ];
    },
  });
}
