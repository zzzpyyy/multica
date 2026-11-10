// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { Editor, Extension, type AnyExtension, type Content } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import {
  createShortcutChord,
  configureShortcutPlatform,
  useShortcutStore,
} from "@multica/core/shortcuts";
import { PatchedListItem } from "./list-item";
import {
  createSubmitShortcutExtension,
  shouldReplayNativeEnter,
  submitKeyAction,
  type SubmitShortcutMode,
} from "./submit-shortcut";

function event(
  key: string,
  modifiers: Partial<Pick<KeyboardEvent, "metaKey" | "ctrlKey" | "altKey" | "shiftKey" | "repeat">> = {},
): KeyboardEvent {
  return new KeyboardEvent("keydown", {
    key,
    bubbles: true,
    cancelable: true,
    ...modifiers,
  });
}

function press(editor: Editor, keyboardEvent: KeyboardEvent): boolean {
  let handled = false;
  editor.view.someProp("handleKeyDown", (handler) => {
    if (!handler(editor.view, keyboardEvent)) return false;
    handled = true;
    return true;
  });
  return handled;
}

function makeEditor(
  content: Content,
  onSubmit: () => void,
  extraExtensions: AnyExtension[] = [],
  mode: SubmitShortcutMode = "message",
) {
  return new Editor({
    element: document.createElement("div"),
    content,
    extensions: [
      StarterKit.configure({ listItem: false }),
      PatchedListItem,
      Markdown,
      createSubmitShortcutExtension(() => {
        onSubmit();
        return true;
      }, { mode }),
      ...extraExtensions,
    ],
  });
}

const editors: Editor[] = [];

beforeEach(() => {
  configureShortcutPlatform("windows");
});

afterEach(() => {
  editors.splice(0).forEach((editor) => editor.destroy());
  useShortcutStore.getState().resetAll();
  configureShortcutPlatform(null);
});

describe("submit shortcut matching", () => {
  const ctrlEnter = createShortcutChord("Enter", { primary: true });

  it("handles the configured shortcut exactly", () => {
    expect(
      submitKeyAction(event("Enter", { ctrlKey: true }), "message", {
        configuredShortcut: ctrlEnter,
      }),
    ).toBe("submit");
    expect(
      submitKeyAction(event("Enter", { ctrlKey: true, shiftKey: true }), "message", {
        configuredShortcut: ctrlEnter,
      }),
    ).toBeNull();
    // Send = Mod+Enter: plain Enter stays a newline (C15).
    expect(
      submitKeyAction(event("Enter"), "message", { configuredShortcut: ctrlEnter }),
    ).toBeNull();
  });

  it("also sends on Mod+Enter when Send is plain Enter (C7)", () => {
    const enter = createShortcutChord("Enter");
    expect(submitKeyAction(event("Enter"), "message", { configuredShortcut: enter })).toBe("submit");
    expect(
      submitKeyAction(event("Enter", { ctrlKey: true }), "message", { configuredShortcut: enter }),
    ).toBe("submit");
    expect(
      submitKeyAction(event("Enter", { shiftKey: true }), "message", { configuredShortcut: enter }),
    ).toBeNull();
  });

  it("never submits when Send is unbound", () => {
    expect(submitKeyAction(event("Enter"), "message", { configuredShortcut: null })).toBeNull();
  });

  it("long text is a fixed Mod+Enter whatever Send says (C11, C12, C15)", () => {
    for (const configuredShortcut of [createShortcutChord("Enter"), ctrlEnter, null]) {
      expect(submitKeyAction(event("Enter"), "longText", { configuredShortcut })).toBeNull();
      expect(
        submitKeyAction(event("Enter", { ctrlKey: true }), "longText", { configuredShortcut }),
      ).toBe("submit");
    }
  });

  it("does not submit while an IME composition is active", () => {
    expect(
      submitKeyAction(event("Enter"), "message", {
        configuredShortcut: createShortcutChord("Enter"),
        composing: true,
      }),
    ).toBeNull();
  });

  it("ignores Safari's IME commit event in both modes (C3, C16)", () => {
    for (const mode of ["message", "longText"] as const) {
      const safariImeCommit = event("Enter", { ctrlKey: true });
      Object.defineProperty(safariImeCommit, "keyCode", { value: 229 });
      expect(
        submitKeyAction(safariImeCommit, mode, { configuredShortcut: ctrlEnter }),
      ).toBeNull();
    }
  });

  it("swallows a held submit key instead of submitting again (C6)", () => {
    expect(
      submitKeyAction(event("Enter", { repeat: true }), "message", {
        configuredShortcut: createShortcutChord("Enter"),
      }),
    ).toBe("swallow");
    expect(
      submitKeyAction(event("Enter", { ctrlKey: true, repeat: true }), "longText"),
    ).toBe("swallow");
    // A held non-submit key is none of our business.
    expect(
      submitKeyAction(event("Enter", { repeat: true }), "longText"),
    ).toBeNull();
  });

  it("only remaps Shift+Enter when plain Enter is Send", () => {
    expect(
      shouldReplayNativeEnter(
        event("Enter", { shiftKey: true }),
        createShortcutChord("Enter"),
        false,
      ),
    ).toBe(true);
    expect(
      shouldReplayNativeEnter(
        event("Enter", { shiftKey: true }),
        createShortcutChord("Enter", { primary: true }),
        false,
      ),
    ).toBe(false);
  });
});

describe("Send = Enter editor behavior", () => {
  it("lets an open high-priority suggestion picker consume Enter first", () => {
    useShortcutStore.getState().setShortcut("send", createShortcutChord("Enter"));
    let submitCount = 0;
    let acceptCount = 0;
    const picker = Extension.create({
      name: "testSuggestionPicker",
      priority: 101,
      addProseMirrorPlugins() {
        return [new Plugin({
          props: {
            handleKeyDown: (_view, keyboardEvent) => {
              if (keyboardEvent.key !== "Enter") return false;
              acceptCount += 1;
              return true;
            },
          },
        })];
      },
    });
    const editor = makeEditor(
      "@al",
      () => { submitCount += 1; },
      [picker],
    );
    editors.push(editor);
    editor.commands.focus("end");

    expect(press(editor, event("Enter"))).toBe(true);
    expect(acceptCount).toBe(1);
    expect(submitCount).toBe(0);
  });

  it("submits on Enter and turns Shift+Enter into the old paragraph Enter", () => {
    useShortcutStore.getState().setShortcut("send", createShortcutChord("Enter"));
    let submitCount = 0;
    const editor = makeEditor("hello", () => { submitCount += 1; });
    editors.push(editor);
    editor.commands.focus("end");

    expect(press(editor, event("Enter"))).toBe(true);
    expect(submitCount).toBe(1);
    expect(editor.state.doc.childCount).toBe(1);

    expect(press(editor, event("Enter", { shiftKey: true }))).toBe(true);
    expect(submitCount).toBe(1);
    expect(editor.state.doc.childCount).toBe(2);
    let hasHardBreak = false;
    editor.state.doc.descendants((node) => {
      if (node.type.name === "hardBreak") hasHardBreak = true;
    });
    expect(hasHardBreak).toBe(false);
  });

  it("continues list items with Shift+Enter", () => {
    useShortcutStore.getState().setShortcut("send", createShortcutChord("Enter"));
    let submitCount = 0;
    const editor = makeEditor(
      {
        type: "doc",
        content: [{
          type: "bulletList",
          content: [{
            type: "listItem",
            content: [{ type: "paragraph", content: [{ type: "text", text: "one" }] }],
          }],
        }],
      },
      () => { submitCount += 1; },
    );
    editors.push(editor);
    editor.commands.focus("end");

    expect(press(editor, event("Enter", { shiftKey: true }))).toBe(true);
    expect(submitCount).toBe(0);
    expect(editor.state.doc.firstChild?.childCount).toBe(2);
  });

  it("inserts the original code-block newline with Shift+Enter", () => {
    useShortcutStore.getState().setShortcut("send", createShortcutChord("Enter"));
    let submitCount = 0;
    const editor = makeEditor(
      {
        type: "doc",
        content: [{
          type: "codeBlock",
          attrs: { language: null },
          content: [{ type: "text", text: "line" }],
        }],
      },
      () => { submitCount += 1; },
    );
    editors.push(editor);
    editor.commands.focus("end");

    expect(press(editor, event("Enter"))).toBe(true);
    expect(submitCount).toBe(1);
    expect(press(editor, event("Enter", { shiftKey: true }))).toBe(true);
    expect(editor.state.doc.firstChild?.textContent).toBe("line\n");
  });
});

describe("default send and long-text editors", () => {
  it("defaults Send to plain Enter", () => {
    let submitCount = 0;
    const editor = makeEditor("hello", () => { submitCount += 1; });
    editors.push(editor);
    editor.commands.focus("end");

    expect(press(editor, event("Enter"))).toBe(true);
    expect(submitCount).toBe(1);
  });

  it("a held Enter submits once and inserts nothing (C6)", () => {
    let submitCount = 0;
    const editor = makeEditor("x", () => { submitCount += 1; });
    editors.push(editor);
    editor.commands.focus("end");

    expect(press(editor, event("Enter"))).toBe(true);
    for (let i = 0; i < 5; i += 1) {
      expect(press(editor, event("Enter", { repeat: true }))).toBe(true);
    }
    expect(submitCount).toBe(1);
    expect(editor.state.doc.childCount).toBe(1);
  });

  it("long text: Enter is a new paragraph, Mod+Enter submits (C11)", () => {
    let submitCount = 0;
    const editor = makeEditor("a", () => { submitCount += 1; }, [], "longText");
    editors.push(editor);
    editor.commands.focus("end");

    press(editor, event("Enter"));
    expect(submitCount).toBe(0);
    expect(editor.state.doc.childCount).toBe(2);
    expect(press(editor, event("Enter", { ctrlKey: true }))).toBe(true);
    expect(submitCount).toBe(1);
  });

  it("an editor with no submit action keeps every Enter for editing (C13)", () => {
    const editor = new Editor({
      element: document.createElement("div"),
      content: "a",
      extensions: [
        StarterKit.configure({ listItem: false }),
        PatchedListItem,
        Markdown,
        createSubmitShortcutExtension(() => false, { canSubmit: () => false }),
      ],
    });
    editors.push(editor);
    editor.commands.focus("end");

    press(editor, event("Enter"));
    press(editor, event("Enter", { repeat: true }));
    // Both reached the editor's own Enter: two new paragraphs.
    expect(editor.state.doc.childCount).toBe(3);
  });
});
