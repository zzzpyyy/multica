// Role multi-instance (fork, PRD D). Shapes follow the agreed contract
// (plan v2.2 §3.2) field for field.

export interface CreateInstancesRequest {
  agent_id: string;
  splits: string[];           // k = splits.length; each split must be non-empty after trimming
  priority?: boolean;         // members only
}

export type InstanceStatus = "queued" | "running" | "completed" | "failed" | "cancelled";
// running covers the task statuses dispatched / running / waiting_local_directory

export type InstanceQueueReason = "role_full" | "runtime_full" | "runtime_offline" | "waiting_dispatch";

export interface TaskInstance {
  index: number;              // 1-based
  split: string;
  status: InstanceStatus;     // status of the latest attempt
  task_id: string;            // latest attempt
  task_ids: string[];         // every attempt, oldest first; a comment whose source_task_id is here gets "instance index/total"
  session_id: string | null;   // written back by the daemon once the run starts; may be null for the first seconds
  work_dir: string | null;
  branch_name: string | null;
  started_at: string | null;
  completed_at: string | null;
  queue: {                    // only set when status=queued
    position: number;         // place among all queued requests for this role, 1-based
    reason: InstanceQueueReason;
    running: number | null;   // role_full: the role's running count; runtime_full: the runtime's running count
    limit: number | null;     // the matching limit N, or the runtime limit
  } | null;
}

export interface TaskInstanceBatch {
  id: string;
  issue_id: string;
  agent: { id: string; name: string };
  total: number;
  priority: boolean;          // derived from the instance rows' priority >= 10, not stored
  requested_by: { type: "member" | "agent"; id: string; name: string };
  created_at: string;
  settled_at: string | null;  // when every instance ended and the merger was woken
  instances: TaskInstance[];
}
export interface ListInstancesResponse { batches: TaskInstanceBatch[] }
