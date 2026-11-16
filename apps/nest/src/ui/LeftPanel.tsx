// The left column (PRD v4 3.1): every top-level issue in three groups.
import { useState } from "react";
import type { Issue } from "../api";
import { actorName, childrenOf, leftGroups, oneLine, runCounts, statusText } from "../model";
import { go, issuePath } from "../router";
import { useWorkspace } from "./context";
import { Dotted, Progress, ProgressLine } from "./Progress";

export function LeftPanel({ selectedTopId }: { selectedTopId: string | null }) {
  const ws = useWorkspace();
  const groups = leftGroups(ws.tree, ws);
  const [showDone, setShowDone] = useState(false);
  return (
    <nav className="left" aria-label="顶层任务">
      {groups.waiting.length > 0 && <Group title="等你处理" issues={groups.waiting} selected={selectedTopId} />}
      {groups.active.length > 0 && <Group title="进行中" issues={groups.active} selected={selectedTopId} />}
      {groups.done.length > 0 && (
        <section className="group" data-group="已完成">
          <button type="button" className="group-title link-button" aria-expanded={showDone} onClick={() => setShowDone((v) => !v)}>
            已完成 {groups.done.length} 个
          </button>
          {showDone && groups.done.map((i) => <Entry key={i.id} issue={i} selected={selectedTopId === i.id} />)}
        </section>
      )}
    </nav>
  );
}

function Group({ title, issues, selected }: { title: string; issues: Issue[]; selected: string | null }) {
  return (
    <section className="group" data-group={title}>
      <h2 className="group-title">{title}</h2>
      {issues.map((i) => <Entry key={i.id} issue={i} selected={selected === i.id} />)}
    </section>
  );
}

function Entry({ issue, selected }: { issue: Issue; selected: boolean }) {
  const ws = useWorkspace();
  const kids = childrenOf(ws.tree, issue.id);
  const parts = oneLine(issue, kids, ws.att, ws.cat);
  const assignee = actorName(ws.names, issue.assignee_type, issue.assignee_id);
  return (
    <a
      href={issuePath(ws.slug, issue.id)}
      className={`entry${selected ? " selected" : ""}`}
      data-entry={issue.identifier}
      aria-current={selected ? "true" : undefined}
      onClick={(e) => { e.preventDefault(); go(issuePath(ws.slug, issue.id)); }}
    >
      <span className="entry-top">
        <span className="ident">{issue.identifier}</span>
        <span className="title">{issue.title}</span>
      </span>
      <span className="entry-meta">
        <span className="status">{statusText(issue, ws)}</span>
        {assignee && <span>{assignee}</span>}
        <Progress kids={kids} />
        {runCounts(ws.runs, issue.id, ws.names).map((c) => <span key={c} className="runs"><Dotted text={c} /></span>)}
      </span>
      <ProgressLine parts={parts} />
    </a>
  );
}
