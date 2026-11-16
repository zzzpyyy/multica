import { describe, expect, it } from "vitest";
import { keyAction, type KeyLike } from "./composer";

const key = (k: Partial<KeyLike> = {}): KeyLike => ({
  key: "Enter", shiftKey: false, metaKey: false, ctrlKey: false, repeat: false, isComposing: false, keyCode: 13, ...k,
});

describe("keyAction (PRD v4 section 5)", () => {
  it("C1: Enter sends", () => expect(keyAction(key(), "hello")).toBe("send"));
  it("C2: Shift+Enter is a line break", () => expect(keyAction(key({ shiftKey: true }), "a")).toBe("newline"));
  it("C3, C4: composing, and Safari's trailing 229, go to the input method", () => {
    expect(keyAction(key({ isComposing: true }), "nihao")).toBe("ime");
    expect(keyAction(key({ keyCode: 229 }), "你好")).toBe("ime");
  });
  it("C5, C6: text around blank lines still sends", () => expect(keyAction(key(), "\n a \n")).toBe("send"));
  it("C7: Cmd/Ctrl+Enter sends", () => {
    expect(keyAction(key({ metaKey: true }), "x")).toBe("send");
    expect(keyAction(key({ ctrlKey: true }), "x")).toBe("send");
  });
  it("C8: blank text never sends and inserts nothing", () => {
    expect(keyAction(key(), "  \n ")).toBe("swallow");
  });
  it("a held Enter sends once", () => expect(keyAction(key({ repeat: true }), "x")).toBe("swallow"));
  it("other keys pass", () => expect(keyAction(key({ key: "a", keyCode: 65 }), "x")).toBe("pass"));
});
