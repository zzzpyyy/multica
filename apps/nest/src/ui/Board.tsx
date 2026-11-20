// Layout A: the whole workspace as one tree. The first level is every
// top-level issue in the three groups of PRD v4 3.1; the one holding the
// selected issue is open, the others are one row each. Sub-issues follow by
// stage (PRD v4 3.2.2); the selected issue opens in place under its subtree.
import { useEffect, useRef, useState, type ReactNode } from "react";
import type { Issue } from "../api";
import {
  actorName,
  ancestors,
  childrenOf,
  foldedLabel,
  leftGroups,
  needsYou,
  oneLine,
  runCounts,
  shownStatus,
  stageRows,
  stripTitle,
  tone,
  visibleIssues,
  waitingOn,
} from "../model";
import { go, issuePath } from "../router";
import { useWorkspace } from "./context";
import { IssueView } from "./IssueView";
import { Dotted, Progress, ProgressLine, Segments } from "./Progress";
import type { Filter } from "./TopBar";

interface Shared {
  current: Issue | null;
  commentId: string | null;
  /** Shown issues while a filter is on; null shows everything. */
  visible: Set<string> | null;
  /** The selected issue and its ancestors: what holds them stays open. */
  path: string[];
  isOpen: (issue: Issue, depth: number) => boolean;
  toggle: (issue: Issue, depth: number) => void;
}

export function Board({ current, commentId, filter }: { current: Issue | null; commentId: string | null; filter: Filter }) {
  const ws = useWorkspace();
  const { tree } = ws;
  const g = leftGroups(tree, ws);
  const text = filter.text.trim().toLowerCase();
  const visible =
    text || filter.mine
      ? visibleIssues(tree, (i) =>
          (!text || i.identifier.toLowerCase().includes(text) || i.title.toLowerCase().includes(text)) && (!filter.mine || needsYou(i, ws.att)))
      : null;

  // Open/closed by hand, in memory only. Without one, a top-level issue is
  // open when it holds the selected issue and anything deeper is open.
  const [manual, setManual] = useState<Map<string, boolean>>(new Map());
  const [showDone, setShowDone] = useState(false);
  const path = current ? [...ancestors(tree, current), current].map((i) => i.id) : [];
  const pathKey = path.join("/");
  // Selecting an issue opens the way to it, whatever was closed before.
  useEffect(() => {
    setManual((m) => {
      if (!path.some((id) => m.has(id))) return m;
      const n = new Map(m);
      for (const id of path) n.delete(id);
      return n;
    });
  }, [pathKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const shared: Shared = {
    current,
    commentId,
    visible,
    path,
    isOpen: (issue, depth) => visible !== null || (manual.get(issue.id) ?? (depth > 0 || path.includes(issue.id))),
    toggle: (issue, depth) => setManual((m) => new Map(m).set(issue.id, !shared.isOpen(issue, depth))),
  };
  const shown = (list: Issue[]) => (visible ? list.filter((i) => visible.has(i.id)) : list);
  const waiting = shown(g.waiting);
  const active = shown(g.active);
  const done = shown(g.done);
  const doneOpen = showDone || !!visible || done.some((i) => path.includes(i.id));

  if (tree.roots.length === 0) return <p className="empty">现在没有进行中的任务，在右边跟 super-agent 说一件事就行</p>;
  if (visible && waiting.length + active.length + done.length === 0) return <p className="empty">没有符合条件的任务</p>;
  return (
    <>
      {waiting.length > 0 && <Group title="等你处理" issues={waiting} shared={shared} />}
      {active.length > 0 && <Group title="进行中" issues={active} shared={shared} />}
      {done.length > 0 && (
        <section className="group" data-group="已完成">
          <button type="button" className="group-title link-button" aria-expanded={doneOpen} onClick={() => setShowDone(!doneOpen)}>
            已完成 {done.length} 个
          </button>
          {doneOpen && done.map((i) => <Node key={i.id} issue={i} depth={0} stage={null} parent={null} shared={shared} />)}
        </section>
      )}
    </>
  );
}

function Group({ title, issues, shared }: { title: string; issues: Issue[]; shared: Shared }) {
  return (
    <section className="group" data-group={title}>
      <h2 className="group-title">{title} <span className="count">{issues.length}</span></h2>
      {issues.map((i) => <Node key={i.id} issue={i} depth={0} stage={null} parent={null} shared={shared} />)}
    </section>
  );
}

interface NodeProps {
  issue: Issue;
  depth: number;
  /** The stage number shown at the row's left; null at the top level and for unstaged rows. */
  stage: number | null;
  parent: Issue | null;
  shared: Shared;
}

function Node(props: NodeProps) {
  const { issue, depth, shared } = props;
  const ws = useWorkspace();
  const kids = childrenOf(ws.tree, issue.id);
  const open = kids.length > 0 && shared.isOpen(issue, depth);
  return (
    <div className="node">
      <Row {...props} kids={kids} open={open} />
      {open && <Children parent={issue} depth={depth + 1} shared={shared} />}
      {shared.current?.id === issue.id && (
        <div className="detail">
          <IssueView key={issue.id} issue={issue} commentId={shared.commentId} />
        </div>
      )}
    </div>
  );
}

function Row({ issue, depth, stage, parent, shared, kids, open }: NodeProps & { kids: Issue[]; open: boolean }) {
  const ws = useWorkspace();
  const selected = shared.current?.id === issue.id;
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (selected) ref.current?.scrollIntoView({ block: "nearest" });
  }, [selected]);
  const href = issuePath(ws.slug, issue.id);
  const waiting = waitingOn(issue, ws);
  const status = shownStatus(issue, waiting, ws.cat);
  const parts = kids.length > 0 ? oneLine(issue, kids, ws.att, ws.cat) : [];
  const runs = runCounts(ws.runs, issue.id, ws.names).map((c) => <span key={c} className="runs"><Dotted text={c} /></span>);
  const data = depth === 0 ? { "data-entry": issue.identifier } : { "data-child": issue.identifier };
  const main: ReactNode = (
    <>
      {kids.length > 0 ? (
        <button type="button" className="twisty" aria-expanded={open} aria-label={open ? "折叠" : "展开"} onClick={() => shared.toggle(issue, depth)}>
          {open ? "▾" : "▸"}
        </button>
      ) : (
        <span className="twisty" />
      )}
      {/* The color is a cue only; the status is written out (PRD v4 3.1.3, 3.2.2). */}
      <span className={`dot ${tone(issue, ws.att, ws.cat)}`} aria-hidden />
      <a href={href} className="open" aria-current={selected ? "true" : undefined} onClick={(e) => { e.preventDefault(); go(href); }}>
        <span className="ident">{issue.identifier}</span>
        <span className="title">{parent ? stripTitle(issue.title, parent.identifier) : issue.title}</span>
      </a>
      <span className="state" title={status ?? undefined}>{status}</span>
      <Who issue={issue} />
    </>
  );
  const tag = waiting && <span className={`tag ${tone(issue, ws.att, ws.cat)}`}>{waiting}</span>;

  // An open top-level issue heads its block: title, one cell per sub-issue, the summary.
  if (depth === 0 && open) {
    return (
      <div ref={ref} className={`root-head${selected ? " selected" : ""}`} {...data}>
        <div className="line">
          {main}
          {tag}
        </div>
        <div className="summary">
          <Segments kids={kids} />
          <Progress kids={kids} label="已关闭 " />
          {runs}
        </div>
        <ProgressLine parts={parts} />
      </div>
    );
  }
  return (
    <>
      <div ref={ref} className={`node-row${depth === 0 ? " root" : ""}${selected ? " selected" : ""}`} {...data}>
        {depth > 0 && <span className="stage-no">{stage ?? ""}</span>}
        {main}
        <span className="tags">
          {tag}
          {runs}
          <Progress kids={kids} />
        </span>
      </div>
      {parts.length > 0 && (
        <div className={`row-line${depth === 0 ? " root" : ""}`}>
          <ProgressLine parts={parts} />
        </div>
      )}
    </>
  );
}

/** Sub-issues by stage; adjacent closed stages fold into one line (N13). */
function Children({ parent, depth, shared }: { parent: Issue; depth: number; shared: Shared }) {
  const ws = useWorkspace();
  const all = childrenOf(ws.tree, parent.id);
  const kids = shared.visible ? all.filter((k) => shared.visible!.has(k.id)) : all;
  const [unfolded, setUnfolded] = useState<Set<string>>(new Set());
  const node = (c: Issue, stage: number | null) => <Node key={c.id} issue={c} depth={depth} stage={stage} parent={parent} shared={shared} />;
  return (
    <div className="kids">
      {stageRows(kids, ws.cat).flatMap((row) => {
        if (row.kind === "group") return row.issues.map((c) => node(c, row.stage));
        const id = `f${row.from}`;
        // Like 已完成: the stage holding the selected issue stays open (BUNN-31 问题 2).
        const open = !!shared.visible || unfolded.has(id) || row.groups.some((g) => g.issues.some((c) => shared.path.includes(c.id)));
        return [
          <button
            key={id}
            type="button"
            className="folded-row"
            aria-expanded={open}
            onClick={() => setUnfolded((s) => { const n = new Set(s); if (!n.delete(id)) n.add(id); return n; })}
          >
            <Dotted text={foldedLabel(row)} />
          </button>,
          ...(open ? row.groups.flatMap((g) => g.issues.map((c) => node(c, g.stage))) : []),
        ];
      })}
    </div>
  );
}

/** Round initial and name; an empty slot without an assignee keeps the columns aligned. */
function Who({ issue }: { issue: Issue }) {
  const ws = useWorkspace();
  const name = actorName(ws.names, issue.assignee_type, issue.assignee_id);
  return (
    <span className="who">
      {name && (
        <>
          <span className="avatar" aria-hidden>{Array.from(name)[0]}</span>
          <span className="who-name" title={name}>{name}</span>
        </>
      )}
    </span>
  );
}
