// Modified in zzzpyyy/multica (fork of multica-ai/multica v0.8.1). See MODIFICATIONS.md.
/**
 * Hide the reserved agent-to-server quick-action footer while a response is
 * still streaming. Persisted message content is already stripped server-side;
 * this closes the short window where an incomplete JSON fence could otherwise
 * flash in the live timeline.
 */
export function stripChatQuickActionsProtocol(content: string): string {
  return stripTrailingProtocolFence(content, "quick-actions");
}

/**
 * Fork (BUNN-28 §3.7): the super-agent's `super-action` block, same rule —
 * only a trailing fence is the protocol; the server strips the persisted copy.
 * It is always last, so strip it before the quick-actions footer.
 */
export function stripChatProtocolFences(content: string): string {
  return stripChatQuickActionsProtocol(stripTrailingProtocolFence(content, "super-action"));
}

function stripTrailingProtocolFence(content: string, fence: string): string {
  const matches = [
    ...content.matchAll(new RegExp(`(?:^|\\r?\\n)\`\`\`${fence}(?:\\r?\\n|$)`, "g")),
  ];
  const match = matches.at(-1);
  if (!match || match.index == null) return content;

  const footerStart = match.index;
  const bodyStart = footerStart + match[0].length;
  const closingFence = /\r?\n```/.exec(content.slice(bodyStart));
  if (closingFence) {
    const afterFence = content.slice(
      bodyStart + closingFence.index + closingFence[0].length,
    );
    // Once later prose arrives, this was an ordinary example in the reply,
    // not the reserved trailing footer.
    if (afterFence.trim() !== "") return content;
  }

  return content.slice(0, footerStart).trimEnd();
}
