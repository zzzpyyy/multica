// The selected issue, opened in place in the tree (layout A), under its row
// and its sub-issues: ancestors, status, who it waits on, instances,
// description and comments. Its row already shows the title, assignee and
// progress.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { ApiError, approve, type Issue, type InstanceBatch } from "../api";
import { ancestors, runCounts, shownStatus, waitingOn } from "../model";
import { keys, q } from "../queries";
import { go, issuePath } from "../router";
import { Comments } from "./Comments";
import { useWorkspace } from "./context";
import { Markdown } from "./Markdown";
import { Dotted } from "./Progress";

export function IssueView({ issue, commentId }: { issue: Issue; commentId: string | null }) {
  const ws = useWorkspace();
  const { tree, slug } = ws;
  const path = ancestors(tree, issue);
  const waiting = waitingOn(issue, ws);
  const status = shownStatus(issue, waiting, ws.cat);

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
      <header className="issue-head meta">
        {/* The plain label, and none when 在等谁 says it already (待规划, 阻塞，等你处理). */}
        {status && <span className="status" data-status={issue.status}>{status}</span>}
        {waiting && <span className="waiting" data-testid="waiting">{waiting}</span>}
        {runCounts(ws.runs, issue.id, ws.names).map((c) => <span key={c} className="runs"><Dotted text={c} /></span>)}
        {ws.att.mine.has(issue.id) && issue.status === "in_review" && <Approve issue={issue} />}
      </header>
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
