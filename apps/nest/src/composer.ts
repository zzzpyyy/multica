// Enter-to-send for the three message boxes: comment, reply, chat (PRD v4
// section 5, C1–C8). Pure, so the rules are tested without a browser.

export interface KeyLike {
  key: string;
  shiftKey: boolean;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey?: boolean;
  repeat: boolean;
  isComposing: boolean;
  keyCode: number;
}

/**
 * - `ime`: an input method is composing; leave the key to it (C3, C4).
 *   Safari sends the Enter that ends composition with keyCode 229.
 * - `newline`: Shift+Enter, the browser's own line break (C2).
 * - `send`: Enter, or ⌘/Ctrl+Enter, with text that is not blank (C1, C5–C7).
 * - `swallow`: an Enter that must not insert a line or send — blank text
 *   (C8) or a key held down (only the first press sends).
 * - `pass`: any other key.
 */
export type KeyAction = "ime" | "newline" | "send" | "swallow" | "pass";

export function keyAction(e: KeyLike, text: string): KeyAction {
  if (e.isComposing || e.keyCode === 229) return "ime";
  if (e.key !== "Enter") return "pass";
  if (e.shiftKey && !e.metaKey && !e.ctrlKey) return "newline";
  if (e.altKey) return "pass";
  if (e.repeat) return "swallow";
  return text.trim() === "" ? "swallow" : "send";
}

/** localStorage key of a draft (plan 6.5). `scope` is "chat", or "<issue id>:<root | parent comment id>". */
export const draftKey = (workspaceId: string, scope: string) => `nest:draft:${workspaceId}:${scope}`;

export function readDraft(key: string): string {
  try {
    return localStorage.getItem(key) ?? "";
  } catch {
    return "";
  }
}

export function writeDraft(key: string, text: string): void {
  try {
    if (text) localStorage.setItem(key, text);
    else localStorage.removeItem(key);
  } catch {
    // storage full or disabled: the draft just does not survive a reload
  }
}
