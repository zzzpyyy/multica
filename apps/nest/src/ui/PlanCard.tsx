// Plan cards under a super-agent message (PRD v4 section 4 rule 4).
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, decidePlanCard, type PlanCard } from "../api";
import { keys } from "../queries";
import { go, issuePath } from "../router";
import { useWorkspace } from "./context";

const STATUS: Record<PlanCard["status"], string> = {
  pending: "待确认",
  creating: "正在创建…",
  created: "已创建",
  failed: "创建失败",
  cancelled: "已取消",
  expired: "已失效",
};
const AFTER: Record<string, string> = { todo: "待办", backlog: "待规划" };

export function PlanCardView({ card, sessionId }: { card: PlanCard; sessionId: string }) {
  const { slug } = useWorkspace();
  const qc = useQueryClient();
  // Both buttons lock at the first click; the server runs a card once.
  const [deciding, setDeciding] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const decide = async (decision: "confirm" | "cancel") => {
    setDeciding(true);
    setError(null);
    try {
      const next = await decidePlanCard(slug, card.id, decision);
      qc.setQueryData<PlanCard[]>(keys.planCards(slug, sessionId), (old) => old?.map((c) => (c.id === next.id ? next : c)));
    } catch (e) {
      if (!(e instanceof ApiError && e.status === 409)) setError("没提交成功，再点一次");
      setDeciding(false);
    } finally {
      void qc.invalidateQueries({ queryKey: keys.planCards(slug, sessionId) });
    }
  };
  const open = (id: string) => go(issuePath(slug, id));

  return (
    <section className={`plan-card status-${card.status}`} data-card={card.id}>
      <header>
        <strong>{card.kind === "create_issues" ? "建任务" : "加开实例"}</strong>
        <span className="badge">{STATUS[card.status]}</span>
      </header>
      {card.kind === "create_issues" ? (
        <ol>
          {(card.items ?? []).map((item, i) => (
            <li key={i}>
              <p><strong>{item.title}</strong></p>
              <dl>
                <dt>负责人</dt><dd>{item.assignee.name}</dd>
                <dt>要做什么</dt><dd>{item.what}</dd>
                <dt>怎样算完成</dt><dd>{item.done}</dd>
                <dt>建好后</dt><dd>{AFTER[item.status] ?? item.status} · {item.will_run ? "会运行" : "不会运行"}</dd>
              </dl>
            </li>
          ))}
        </ol>
      ) : (
        <dl>
          <dt>任务</dt><dd>{card.issue?.identifier} {card.issue?.title}</dd>
          <dt>角色</dt><dd>{card.agent?.name}</dd>
          <dt>分工</dt>
          <dd><ol>{(card.splits ?? []).map((s, i) => <li key={i}>{s}</li>)}</ol></dd>
          {card.priority && <><dt>优先</dt><dd>是</dd></>}
        </dl>
      )}
      {card.status === "created" && (card.created_issues?.length ?? 0) > 0 && (
        <p>
          已创建：
          {card.created_issues!.map((c) => (
            <a key={c.id} href={issuePath(slug, c.id)} onClick={(e) => { e.preventDefault(); open(c.id); }}>{c.identifier} </a>
          ))}
        </p>
      )}
      {card.status === "failed" && card.error && <p className="error">{card.error}</p>}
      {card.status === "pending" && (
        <div className="card-actions">
          <button type="button" className="primary" disabled={deciding} onClick={() => void decide("confirm")}>确认</button>
          <button type="button" disabled={deciding} onClick={() => void decide("cancel")}>取消</button>
        </div>
      )}
      {error && <p className="error">{error}</p>}
    </section>
  );
}
