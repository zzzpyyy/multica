DROP TRIGGER IF EXISTS fork_super_agent_run_guard ON agent_task_queue;
DROP FUNCTION IF EXISTS fork_super_agent_run_guard();
DROP TABLE IF EXISTS super_agent_question;
DROP TABLE IF EXISTS super_agent_push;
DROP TABLE IF EXISTS super_agent_action;
