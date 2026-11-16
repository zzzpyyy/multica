// Addresses Nest understands (plan section 5), on the browser's History API.
// The server writes three kinds of relative links into comments and pushes:
// `mention://issue/<id>` (links.ts), `/<slug>/issues/<id>#comment-<id>` and
// `/<slug>/chat?session=<id>&message=<id>`; all of them open inside Nest.

export type Route =
  | { kind: "root" }
  /** `/<slug>/issues`: pick the default issue. */
  | { kind: "issues"; slug: string }
  | { kind: "issue"; slug: string; issueId: string; commentId: string | null }
  | { kind: "chat"; slug: string; sessionId: string | null; messageId: string | null }
  /** Anything else under a slug: goes to that workspace's issues page. */
  | { kind: "unknown"; slug: string };

export function parse(loc: { pathname: string; search: string; hash: string }): Route {
  const parts = loc.pathname.split("/").filter(Boolean).map((p) => {
    try {
      return decodeURIComponent(p);
    } catch {
      return p;
    }
  });
  const [slug, section, id, ...rest] = parts;
  if (!slug) return { kind: "root" };
  if (section === "issues" && rest.length === 0) {
    if (!id) return { kind: "issues", slug };
    const m = /^#comment-(.+)$/.exec(loc.hash);
    return { kind: "issue", slug, issueId: id, commentId: m ? m[1]! : null };
  }
  if (section === "chat" && !id) {
    const q = new URLSearchParams(loc.search);
    return { kind: "chat", slug, sessionId: q.get("session"), messageId: q.get("message") };
  }
  return { kind: "unknown", slug };
}

export const issuesPath = (slug: string) => `/${encodeURIComponent(slug)}/issues`;
export const issuePath = (slug: string, issueId: string, commentId?: string | null) =>
  `${issuesPath(slug)}/${encodeURIComponent(issueId)}${commentId ? `#comment-${commentId}` : ""}`;

const listeners = new Set<() => void>();
export function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
const notify = () => listeners.forEach((fn) => fn());
if (typeof window !== "undefined") window.addEventListener("popstate", notify);

/** A new history entry: the back button returns to where you were (N14). */
export function go(path: string): void {
  const here = location.pathname + location.search + location.hash;
  if (path === here) return notify();
  history.pushState(null, "", path);
  notify();
}

export function replace(path: string): void {
  history.replaceState(null, "", path);
  notify();
}

/** Whether a same-origin path is one Nest routes itself, not a file or API URL. */
export function isAppPath(pathname: string): boolean {
  const route = parse({ pathname, search: "", hash: "" });
  return route.kind === "issue" || route.kind === "issues" || route.kind === "chat";
}
