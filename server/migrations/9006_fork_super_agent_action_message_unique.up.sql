-- Fork, B: one action per assistant message (plan 3.5).
CREATE UNIQUE INDEX CONCURRENTLY idx_super_agent_action_message ON super_agent_action (chat_message_id);
