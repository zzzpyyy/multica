// The middle column (PRD v4 3.2): one issue, its ancestors, its sub-issues by
// stage with who each one waits on, instances, description and comments.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { ApiError, approve, type Issue, type InstanceBatch } from "../api";
import {
  actorName,
  ancestors,
  childrenOf,
  foldedLabel,
  oneLine,
  runCounts,
  stageRows,
  statusText,
  stripTitle,
  waitingOn,
} from "../model";
import { keys, q } from "../queries";
import { go, issuePath } from "../router";
import { Comments } from "./Comments";
import { useWorkspace } from "./context";
import { Markdown } from "./Markdown";
import { Progress, ProgressLine } from "./Progress";

export function IssueView({ issue, commentId }: { issue: Issue; commentId: string | null }) {
  const ws = useWorkspace();
  const { tree, slug } = ws;
  const path = ancestors(tree, issue);
  const kids = childrenOf(tree, issue.id);
  const parts = oneLine(issue, kids, ws.att, ws.cat);
  const waiting = waitingOn(issue, ws);
  const assignee = actorName(ws.names, issue.assignee_type, issue.assignee_id);

  return (
    <article className="issue-view" data-issue={issue.identifier}>
      <nav className="crumbs" aria-label="祖先路径">
        {[...path, issue].map((a, i) => (
          <span key={a.id}>
            {i > 0 && " / "}
            {a.id === issue.id ? (
              <span>{a.identifier}</span>
            ) : (
              <a href={issuePath(slug, a.id)} onClick={(e) => { e.preventDefault(); go(issuePath(slug, a.id)); }}>
                {a.identifier}
              </a>
            )}
          </span>
        ))}
      </nav>
      <header className="issue-head">
        <h1>
          <span className="ident">{issue.identifier}</span> {issue.title}
        </h1>
        <div className="meta">
          <span className="status" data-status={issue.status}>{statusText(issue, ws)}</span>
          {assignee && <span>{assignee}</span>}
          {waiting && <span className="waiting" data-testid="waiting">{waiting}</span>}
          {runCounts(ws.runs, issue.id, ws.names).map((c) => <span key={c} className="runs">{c}</span>)}
          <Progress kids={kids} />
          {ws.att.mine.has(issue.id) && issue.status === "in_review" && <Approve issue={issue} />}
        </div>
        {parts.length > 0 && <ProgressLine parts={parts} />}
      </header>
      {kids.length > 0 && <SubIssues parent={issue} />}
      <Instances issueId={issue.id} />
      {issue.description && (
        <section className="description" aria-label="描述">
          <Markdown text={issue.description} />
        </section>
      )}
      <Comments issueId={issue.id} focusCommentId={commentId} />
    </article>
  );
}

function SubIssues({ parent }: { parent: Issue }) {
  const ws = useWorkspace();
  const rows = stageRows(childrenOf(ws.tree, parent.id), ws.cat);
  const [open, setOpen] = useState<Set<string>>(new Set());
  useEffect(() => setOpen(new Set()), [parent.id]);
  return (
    <section className="subissues" aria-label="子任务">
      {rows.map((row) => {
        if (row.kind === "group") {
          return (
            <div key={`g${row.stage}`} className="stage">
              <h4>{row.stage === null ? "未分阶段" : `阶段 ${row.stage}`}</h4>
              {row.issues.map((c) => <SubIssueRow key={c.id} issue={c} parent={parent} />)}
            </div>
          );
        }
        const id = `f${row.from}`;
        return (
          <div key={id} className="stage folded">
            <button
              type="button"
              className="folded-row"
              aria-expanded={open.has(id)}
              onClick={() => setOpen((s) => { const n = new Set(s); if (!n.delete(id)) n.add(id); return n; })}
            >
              {foldedLabel(row)}
            </button>
            {open.has(id) &&
              row.groups.map((g) => (
                <div key={`g${g.stage}`} className="stage">
                  <h4>{g.stage === null ? "未分阶段" : `阶段 ${g.stage}`}</h4>
                  {g.issues.map((c) => <SubIssueRow key={c.id} issue={c} parent={parent} />)}
                </div>
              ))}
          </div>
        );
      })}
    </section>
  );
}

function SubIssueRow({ issue, parent }: { issue: Issue; parent: Issue }) {
  const ws = useWorkspace();
  const kids = childrenOf(ws.tree, issue.id);
  const waiting = waitingOn(issue, ws);
  const assignee = actorName(ws.names, issue.assignee_type, issue.assignee_id);
  return (
    <div className="sub-row" data-child={issue.identifier}>
      <span className="ident">{issue.identifier}</span>
      <a
        href={issuePath(ws.slug, issue.id)}
        className="title"
        onClick={(e) => { e.preventDefault(); go(issuePath(ws.slug, issue.id)); }}
      >
        {stripTitle(issue.title, parent.identifier)}
      </a>
      <span className="status">{statusText(issue, ws)}</span>
      {assignee && <span>{assignee}</span>}
      {waiting && <span className="waiting">{waiting}</span>}
      {runCounts(ws.runs, issue.id, ws.names).map((c) => <span key={c} className="runs">{c}</span>)}
      {kids.length > 0 && <Progress kids={kids} />}
    </div>
  );
}

/** Shown only while the server says you review it; one PUT however often it is clicked. */
function Approve({ issue }: { issue: Issue }) {
  const { slug } = useWorkspace();
  const qc = useQueryClient();
  const [note, setNote] = useState<string | null>(null);
  // A double click delivers two clicks before React re-renders the disabled
  // button: this flag stops the second one synchronously.
  const sent = useRef(false);
  const m = useMutation({
    mutationFn: () => approve(slug, issue),
    onError: (e) => {
      sent.current = false;
      if (e instanceof ApiError && e.status === 409) {
        setNote("状态刚被改过，已刷新");
        void qc.invalidateQueries({ queryKey: keys.issues(slug) });
        void qc.invalidateQueries({ queryKey: keys.attention(slug) });
      } else setNote("没改成，再点一次");
    },
  });
  // Success leaves the button disabled; the issue:updated event and the
  // attention refetch make it disappear.
  return (
    <>
      <button type="button" className="primary" disabled={m.isPending || m.isSuccess} onClick={() => {
          if (sent.current) return;
          sent.current = true;
          setNote(null);
          m.mutate();
        }}>
        验收通过
      </button>
      {note && <span className="note">{note}</span>}
    </>
  );
}

const QUEUE_REASON: Record<string, (q: { running: number | null; limit: number | null }) => string> = {
  role_full: (x) => `角色名额已满（${x.running ?? "?"}/${x.limit ?? "?"}）`,
  runtime_full: () => "运行环境已满",
  runtime_offline: () => "运行环境离线",
  waiting_dispatch: () => "等待领取",
};
const INSTANCE_STATUS: Record<string, string> = {
  queued: "排队中",
  running: "运行中",
  completed: "已完成",
  failed: "失败",
  cancelled: "已取消",
};

function Instances({ issueId }: { issueId: string }) {
  const { slug } = useWorkspace();
  const { data } = useQuery(q.instances(slug, issueId));
  const batches: InstanceBatch[] = data ?? [];
  if (batches.length === 0) return null;
  return (
    <section className="instances" aria-label="实例">
      <h3>实例</h3>
      {batches.map((b) => (
        <div key={b.id} className="batch">
          <p className="muted">{b.agent.name} · {b.total} 个实例</p>
          <ol>
            {b.instances.map((i) => (
              <li key={i.index}>
                #{i.index} {i.split} · {INSTANCE_STATUS[i.status] ?? i.status}
                {i.queue && ` · 第 ${i.queue.position} 位 · ${(QUEUE_REASON[i.queue.reason] ?? (() => i.queue!.reason))(i.queue)}`}
              </li>
            ))}
          </ol>
        </div>
      ))}
    </section>
  );
}
