// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
// @vitest-environment node
import { describe, expect, it } from "vitest";
import { stripChatProtocolFences, stripChatQuickActionsProtocol } from "./quick-actions";

describe("stripChatQuickActionsProtocol", () => {
  it("strips complete and partial trailing protocol blocks", () => {
    expect(
      stripChatQuickActionsProtocol(
        "Answer.\n```quick-actions\n[{\"label\":\"Continue\"}]\n```",
      ),
    ).toBe("Answer.");
    expect(
      stripChatQuickActionsProtocol(
        "Answer.\r\n```quick-actions\r\n[{\"label\":",
      ),
    ).toBe("Answer.");
  });

  it("leaves ordinary content untouched", () => {
    expect(stripChatQuickActionsProtocol("Answer with `quick-actions` inline."))
      .toBe("Answer with `quick-actions` inline.");
    expect(
      stripChatQuickActionsProtocol(
        "Example:\n```quick-actions\n[]\n```\nMore explanation.",
      ),
    ).toBe("Example:\n```quick-actions\n[]\n```\nMore explanation.");
  });
});

// Fork (BUNN-28 §3.7, F6).
describe("stripChatProtocolFences", () => {
  const block = '```super-action\n{"type":"create_issues","issues":[]}\n```';

  it("hides a trailing super-action block, complete or still streaming", () => {
    expect(stripChatProtocolFences(`Please confirm.\n${block}\n`)).toBe("Please confirm.");
    expect(stripChatProtocolFences('Please confirm.\n```super-action\n{"type":"cre')).toBe(
      "Please confirm.",
    );
  });

  it("keeps a super-action fence that is not at the end", () => {
    const content = `Example:\n${block}\nmore text`;
    expect(stripChatProtocolFences(content)).toBe(content);
  });

  it("strips quick-actions followed by super-action", () => {
    const qa = '```quick-actions\n[{"label":"x"}]\n```';
    expect(stripChatProtocolFences(`Done.\n${qa}\n${block}`)).toBe("Done.");
  });

  it("does not match a longer fence name", () => {
    const content = "a\n```super-action-log\nx\n```";
    expect(stripChatProtocolFences(content)).toBe(content);
  });
});
