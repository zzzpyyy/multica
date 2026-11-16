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

/** The one-line progress; each part stays on one line, the calls to action stand out. */
export function ProgressLine({ parts }: { parts: readonly Part[] }) {
  if (parts.length === 0) return null;
  return (
    <p className="oneline" data-testid="oneline">
      {parts.map((part, i) => (
        <span key={i} className={part.callToAction ? "part cta" : "part"}>
          {i > 0 && <span className="sep">{SEPARATOR}</span>}
          {part.text}
        </span>
      ))}
    </p>
  );
}
