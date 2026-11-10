-- Fork, B: an event is pushed to a session once (F8).
CREATE UNIQUE INDEX CONCURRENTLY idx_super_agent_push_dedupe ON super_agent_push (chat_session_id, dedupe_key);
