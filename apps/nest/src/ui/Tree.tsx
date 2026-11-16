// Tree mode (PRD v4 3.3): the left and middle columns become one outline.
import { useState } from "react";
import type { Issue } from "../api";
import { actorName, childrenOf, leftGroups, oneLine, statusText, treeLevel, waitingOn } from "../model";
import { useWorkspace } from "./context";
import { Progress, ProgressLine } from "./Progress";

/** Clicking any row opens it in focus mode. */
export function Tree({ onOpen }: { onOpen: (issue: Issue) => void }) {
  const ws = useWorkspace();
  const groups = leftGroups(ws.tree, ws);
  // Collapsed rows live in memory only: a reload expands everything (N16).
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [showDone, setShowDone] = useState(false);
  const toggle = (id: string) =>
    setCollapsed((s) => {
      const n = new Set(s);
      if (!n.delete(id)) n.add(id);
      return n;
    });
  const props = { collapsed, toggle, onOpen };
  return (
    <div className="tree" role="tree" aria-label="大纲">
      {groups.waiting.length > 0 && (
        <section className="group" data-group="等你处理">
          <h2 className="group-title">等你处理</h2>
          {groups.waiting.map((i) => <Node key={i.id} issue={i} depth={0} {...props} />)}
        </section>
      )}
      {groups.active.length > 0 && (
        <section className="group" data-group="进行中">
          <h2 className="group-title">进行中</h2>
          {groups.active.map((i) => <Node key={i.id} issue={i} depth={0} {...props} />)}
        </section>
      )}
      {groups.done.length > 0 && (
        <section className="group" data-group="已完成">
          <button type="button" className="group-title link-button" aria-expanded={showDone} onClick={() => setShowDone((v) => !v)}>
            已完成 {groups.done.length} 个
          </button>
          {showDone && groups.done.map((i) => <Node key={i.id} issue={i} depth={0} {...props} />)}
        </section>
      )}
    </div>
  );
}

interface NodeProps {
  issue: Issue;
  depth: number;
  collapsed: Set<string>;
  toggle: (id: string) => void;
  onOpen: (issue: Issue) => void;
}

function Node({ issue, depth, collapsed, toggle, onOpen }: NodeProps) {
  const ws = useWorkspace();
  const kids = childrenOf(ws.tree, issue.id);
  const { open, closed } = treeLevel(kids, ws.cat);
  const [showClosed, setShowClosed] = useState(false);
  const isCollapsed = collapsed.has(issue.id);
  const waiting = waitingOn(issue, ws);
  const assignee = actorName(ws.names, issue.assignee_type, issue.assignee_id);
  const parts = kids.length > 0 ? oneLine(issue, kids, ws.att, ws.cat) : [];
  const childProps = { collapsed, toggle, onOpen, depth: depth + 1 };
  return (
    <div className="node" role="treeitem" aria-expanded={kids.length > 0 ? !isCollapsed : undefined} data-node={issue.identifier}>
      <div className="node-row" style={{ paddingLeft: `${depth * 20}px` }}>
        {kids.length > 0 ? (
          <button type="button" className="twisty" aria-label={isCollapsed ? "展开" : "折叠"} onClick={() => toggle(issue.id)}>
            {isCollapsed ? "▸" : "▾"}
          </button>
        ) : (
          <span className="twisty" />
        )}
        <button type="button" className="node-open" onClick={() => onOpen(issue)}>
          <span className="ident">{issue.identifier}</span>
          <span className="title">{issue.title}</span>
          <span className="status">{statusText(issue, ws)}</span>
          {assignee && <span>{assignee}</span>}
          {waiting && <span className="waiting">{waiting}</span>}
          {kids.length > 0 && <Progress kids={kids} />}
        </button>
      </div>
      {parts.length > 0 && (
        <div style={{ paddingLeft: `${depth * 20 + 24}px` }}>
          <ProgressLine parts={parts} />
        </div>
      )}
      {!isCollapsed && (
        <>
          {open.map((c) => <Node key={c.id} issue={c} {...childProps} />)}
          {closed.length > 0 && (
            <div className="closed-row" style={{ paddingLeft: `${(depth + 1) * 20 + 24}px` }}>
              <button type="button" className="link-button" aria-expanded={showClosed} onClick={() => setShowClosed((v) => !v)}>
                已完成 {closed.length} 个
              </button>
            </div>
          )}
          {showClosed && closed.map((c) => <Node key={c.id} issue={c} {...childProps} />)}
        </>
      )}
    </div>
  );
}
