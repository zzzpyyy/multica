// Contract: BUNN-28 plan v2.2 §3.2 (B). Server: fork's super-agent endpoints.
import type { Agent } from "./agent";
import type { ChatSession } from "./chat";

export interface SuperAgentBootstrap extends Agent {
  session: ChatSession;                                // 这个成员唯一的那段会话
}

export type PlanCardStatus =
  | "pending" | "creating" | "created" | "failed" | "cancelled" | "expired";

export interface PlanCardAssignee { type: "member" | "agent" | "squad"; id: string; name: string }

export interface PlanCardIssueItem {
  title: string;
  assignee: PlanCardAssignee;
  what: string;               // 要做什么（规则 5）
  done: string;               // 怎样算完成（规则 5）
  status: "todo" | "backlog";
  will_run: boolean;          // 服务端算：负责人是智能体或小队，且状态不是 backlog
}

interface PlanCardBase {
  id: string;
  chat_session_id: string;
  chat_message_id: string;    // 卡片挂在这条 assistant 消息下面
  status: PlanCardStatus;
  error: string | null;       // 只在 failed 时有值
  created_at: string;
}
export interface CreateIssuesCard extends PlanCardBase {
  kind: "create_issues";
  items: PlanCardIssueItem[];
  created_issues: { id: string; identifier: string }[];   // created 和 failed 时有值，failed 时是已经建好的那部分
}
export interface AddInstancesCard extends PlanCardBase {
  kind: "add_instances";
  issue: { id: string; identifier: string; title: string };
  agent: { id: string; name: string };
  splits: string[];
  priority: boolean;
  batch_id: string | null;    // created 时有值
}
export type PlanCard = CreateIssuesCard | AddInstancesCard;
