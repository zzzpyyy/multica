// What a link or image address in a comment, description or chat message
// becomes (plan section 5, review requirement 2). Same-origin is decided on
// the resolved URL, never by looking at the string: `//host/x` and `/\host/x`
// read like relative paths but resolve to another host.
import { isAppPath } from "./router";

export type LinkKind =
  /** `mention://issue/<id>`: opens in the middle column. */
  | { kind: "issue"; issueId: string }
  /** `@member`, `@agent`, `@squad`, a project: a name, not clickable. */
  | { kind: "name"; actor: "member" | "agent" | "squad" | "project"; id: string }
  /** A Nest page on this origin (`/<slug>/issues/...`, `/<slug>/chat?...`). */
  | { kind: "app"; path: string }
  /** Another same-origin address (`/uploads/...`, `/api/attachments/...`). */
  | { kind: "file"; href: string }
  /** http(s) on another origin: new tab. */
  | { kind: "external"; href: string }
  /** Anything else (`javascript:`, `data:`, unknown mention types, garbage): plain text. */
  | { kind: "text" };

const MENTION = /^mention:\/\/([a-z]+)\/([^/?#\s]+)$/i;

export function classifyLink(href: string | null | undefined, origin: string): LinkKind {
  if (!href) return { kind: "text" };
  const mention = MENTION.exec(href.trim());
  if (mention) {
    const [, type, id] = mention;
    const t = type!.toLowerCase();
    if (t === "issue") return { kind: "issue", issueId: id! };
    if (t === "member" || t === "agent" || t === "squad" || t === "project") return { kind: "name", actor: t, id: id! };
    return { kind: "text" };
  }
  let url: URL;
  try {
    url = new URL(href, origin);
  } catch {
    return { kind: "text" };
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return { kind: "text" };
  if (url.origin === new URL(origin).origin) {
    return isAppPath(url.pathname)
      ? { kind: "app", path: url.pathname + url.search + url.hash }
      : { kind: "file", href: url.href };
  }
  return { kind: "external", href: url.href };
}

/** Images: this origin, or https elsewhere. Returns the resolved src or null. */
export function imageSrc(src: string | null | undefined, origin: string): string | null {
  if (!src) return null;
  let url: URL;
  try {
    url = new URL(src, origin);
  } catch {
    return null;
  }
  if (url.origin === new URL(origin).origin) return url.href;
  return url.protocol === "https:" ? url.href : null;
}
