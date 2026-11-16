// Comments of one issue (PRD v4 3.5, plan 6.3): threads by their root's
// creation time, the newest 20 first, replies under their root.
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ApiError, createComment, listComments, type Comment } from "../api";
import { draftKey } from "../composer";
import { keys, q } from "../queries";
import { Composer, type SendResult } from "./Composer";
import { useWorkspace } from "./context";
import { Markdown } from "./Markdown";

const PAGE = 20;

interface Thread {
  root: Comment;
  replies: Comment[];
}

/** Roots oldest first; a reply whose parent is missing is its own thread. */
export function threads(comments: readonly Comment[]): Thread[] {
  const byId = new Map(comments.map((c) => [c.id, c]));
  const rootOf = (c: Comment): Comment => {
    let cur = c;
    for (let i = 0; i < 32 && cur.parent_id && byId.has(cur.parent_id); i++) cur = byId.get(cur.parent_id)!;
    return cur;
  };
  const map = new Map<string, Thread>();
  const byTime = (a: Comment, b: Comment) => a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id);
  for (const c of [...comments].sort(byTime)) {
    const root = rootOf(c);
    const t = map.get(root.id) ?? { root, replies: [] };
    if (c.id !== root.id) t.replies.push(c);
    map.set(root.id, t);
  }
  return [...map.values()].sort((a, b) => byTime(a.root, b.root));
}

const sameText = (a: string, b: string) => a.trim() === b.trim();

export function Comments({ issueId, focusCommentId }: { issueId: string; focusCommentId: string | null }) {
  const { slug, workspace, userId } = useWorkspace();
  const qc = useQueryClient();
  const { data, isPending, isError, refetch } = useQuery(q.comments(slug, issueId));
  const all = useMemo(() => threads(data?.comments ?? []), [data]);
  const [shown, setShown] = useState(PAGE);
  const [replyTo, setReplyTo] = useState<string | null>(null);
  const [highlight, setHighlight] = useState<string | null>(null);
  const focused = useRef<string | null>(null);

  useEffect(() => {
    setShown(PAGE);
    setReplyTo(null);
  }, [issueId]);

  // #comment-<id>: widen the window to its thread, scroll, highlight once.
  useEffect(() => {
    if (!focusCommentId || !data || focused.current === `${issueId}:${focusCommentId}`) return;
    const index = all.findIndex((t) => t.root.id === focusCommentId || t.replies.some((r) => r.id === focusCommentId));
    if (index < 0) return;
    focused.current = `${issueId}:${focusCommentId}`;
    setShown((n) => Math.max(n, all.length - index));
    setHighlight(focusCommentId);
    requestAnimationFrame(() =>
      document.getElementById(`comment-${focusCommentId}`)?.scrollIntoView({ block: "center" }),
    );
    const t = setTimeout(() => setHighlight(null), 3000);
    return () => clearTimeout(t);
  }, [focusCommentId, data, all, issueId]);

  // Per box: when its first attempt left, so a retry can tell whether that
  // attempt actually landed (the comment endpoint has no idempotency key).
  const firstTry = useRef(new Map<string, string>());
  useEffect(() => firstTry.current.clear(), [issueId]);

  const send = (parentId: string | null) => async (text: string): Promise<SendResult> => {
    const box = parentId ?? "root";
    try {
      const since = firstTry.current.get(box);
      if (since) {
        const fresh = await listComments(slug, issueId);
        const landed = fresh.comments.some(
          (c) => c.author_id === userId && c.parent_id === parentId && sameText(c.content, text) && c.created_at >= since,
        );
        if (landed) {
          qc.setQueryData(keys.comments(slug, issueId), fresh);
          firstTry.current.delete(box);
          if (parentId) setReplyTo(null);
          return { ok: true };
        }
      } else {
        firstTry.current.set(box, new Date(Date.now() - 5000).toISOString());
      }
      const created = await createComment(slug, issueId, text, parentId);
      firstTry.current.delete(box);
      qc.setQueryData<{ comments: Comment[]; truncated: boolean }>(keys.comments(slug, issueId), (old) =>
        old && !old.comments.some((c) => c.id === created.id) ? { ...old, comments: [...old.comments, created] } : old,
      );
      if (parentId) setReplyTo(null);
      return { ok: true };
    } catch (e) {
      return { ok: false, message: e instanceof ApiError && !e.offline ? `没发出去：${e.message}` : "没发出去，再按 Enter 重发" };
    }
  };

  const visible = all.slice(Math.max(0, all.length - shown));
  return (
    <section className="comments" aria-label="评论">
      <h3>评论</h3>
      {isPending && <p className="muted">加载中…</p>}
      {isError && (
        <p className="error">
          评论没加载出来 <button type="button" onClick={() => void refetch()}>重试</button>
        </p>
      )}
      {data?.truncated && <p className="muted">评论太多，只显示了最近的一部分</p>}
      {all.length > visible.length && (
        <button type="button" className="link-button" onClick={() => setShown((n) => n + PAGE)}>
          加载更早的评论
        </button>
      )}
      {visible.map((t) => (
        <div key={t.root.id} className="thread">
          <CommentItem comment={t.root} highlight={highlight === t.root.id} />
          {t.replies.map((r) => (
            <div key={r.id} className="reply">
              <CommentItem comment={r} highlight={highlight === r.id} />
            </div>
          ))}
          {replyTo === t.root.id ? (
            <div className="reply">
              <Composer
                label="回复"
                autoFocus
                draftKey={draftKey(workspace.id, `${issueId}:${t.root.id}`)}
                placeholder="回复，Enter 发送，Shift+Enter 换行"
                onSend={send(t.root.id)}
              />
            </div>
          ) : (
            <button type="button" className="link-button" onClick={() => setReplyTo(t.root.id)}>
              回复
            </button>
          )}
        </div>
      ))}
      <Composer
        label="评论"
        draftKey={draftKey(workspace.id, `${issueId}:root`)}
        placeholder="写评论，Enter 发送，Shift+Enter 换行"
        onSend={send(null)}
      />
    </section>
  );
}

function CommentItem({ comment, highlight }: { comment: Comment; highlight: boolean }) {
  const { names } = useWorkspace();
  const body = useRef<HTMLDivElement>(null);
  const [long, setLong] = useState(false);
  const [open, setOpen] = useState(false);
  const isSuper = comment.author_type === "agent" && comment.author_id === names.superAgentId;
  const kind = isSuper ? "super-agent" : comment.author_type === "agent" ? "智能体" : "成员";
  const who =
    (comment.author_type === "agent" ? names.agent(comment.author_id) : names.member(comment.author_id)) ??
    (comment.author_type === "agent" ? "智能体" : "成员");

  // Longer than 20 rendered lines: collapsed (measured, not counted in source).
  useLayoutEffect(() => {
    const el = body.current;
    if (el) setLong(el.scrollHeight > el.clientHeight + 2);
  }, [comment.content]);

  return (
    <article id={`comment-${comment.id}`} className={`comment${highlight ? " highlight" : ""}`} data-comment-id={comment.id}>
      <header>
        <strong>{who}</strong>
        <span className={`badge badge-${isSuper ? "super" : comment.author_type}`}>{kind}</span>
        <time dateTime={comment.created_at}>{formatTime(comment.created_at)}</time>
      </header>
      {comment.deleted_at ? (
        <p className="muted">这条评论已删除</p>
      ) : (
        <>
          <div ref={body} className={open ? "comment-body" : "comment-body clamp"}>
            <Markdown text={comment.content} />
          </div>
          {long && !open && (
            <button type="button" className="link-button" onClick={() => setOpen(true)}>
              展开全文
            </button>
          )}
          <Attachments comment={comment} />
        </>
      )}
    </article>
  );
}

function Attachments({ comment }: { comment: Comment }) {
  if (comment.attachments.length === 0) return null;
  return (
    <ul className="attachments">
      {comment.attachments.map((a) =>
        a.content_type?.startsWith("image/") ? (
          <li key={a.id}>
            <a href={a.download_url} target="_blank" rel="noopener noreferrer">
              <img src={a.download_url} alt={a.filename} className="thumb" loading="lazy" />
            </a>
          </li>
        ) : (
          <li key={a.id}>
            <a href={a.download_url} download={a.filename}>{a.filename}</a>
          </li>
        ),
      )}
    </ul>
  );
}

export function formatTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? iso
    : d.toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
