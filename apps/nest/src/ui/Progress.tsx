import { Fragment } from "react";
import type { Issue } from "../api";
import { progress, SEPARATOR, type Part } from "../model";
import { useWorkspace } from "./context";

/** Ring, `closed/total` and "含 N 个已取消" over the direct children. */
export function Progress({ kids }: { kids: readonly Issue[] }) {
  const { cat } = useWorkspace();
  const p = progress(kids, cat);
  if (!p) return null;
  const angle = Math.round((p.closed / p.total) * 360);
  return (
    <span className="progress" data-testid="progress">
      <span className="ring" style={{ background: `conic-gradient(var(--accent) ${angle}deg, var(--line) 0)` }} aria-hidden />
      <span>{p.closed}/{p.total}</span>
      {p.cancelled > 0 && <span className="muted">含 {p.cancelled} 个已取消</span>}
    </span>
  );
}

// " · " with the "·" glued to the piece before it, so no line starts with "·".
const GLUED = SEPARATOR.trimEnd();

/**
 * The one-line progress; the calls to action stand out. It wraps only between
 * parts: each part moves down whole, and one wider than the line wraps inside
 * itself, never cut. The text is still the parts joined by " · ".
 */
export function ProgressLine({ parts }: { parts: readonly Part[] }) {
  if (parts.length === 0) return null;
  const last = parts.length - 1;
  return (
    <p className="oneline" data-testid="oneline">
      {parts.map((part, i) => (
        <Fragment key={i}>
          {i > 0 && " "}
          <span className={part.callToAction ? "part cta" : "part"}>
            {part.text}
            {i < last && <span className="sep">{GLUED}</span>}
          </span>
        </Fragment>
      ))}
    </p>
  );
}

/** Text with " · " in it ("TB 运行 2 · 排队 1"), the "·" glued the same way. */
export function Dotted({ text }: { text: string }) {
  const pieces = text.split(SEPARATOR);
  return pieces.map((piece, i) => (
    <Fragment key={i}>
      {i > 0 && " "}
      {piece}
      {i < pieces.length - 1 && <span className="sep">{GLUED}</span>}
    </Fragment>
  ));
}
