-- Fork, B: a comment is handled once per session (no double answers).
CREATE UNIQUE INDEX CONCURRENTLY idx_super_agent_question_comment ON super_agent_question (chat_session_id, comment_id);
