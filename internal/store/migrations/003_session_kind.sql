-- A session's kind says whether an external agent runs it, and the agent's
-- permissions follow the session's mode, so these copies go.
ALTER TABLE sessions DROP COLUMN runtime_id;
ALTER TABLE external_agent_sessions DROP COLUMN approval_policy;
ALTER TABLE external_agent_sessions DROP COLUMN sandbox;
