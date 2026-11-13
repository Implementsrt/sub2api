-- Fork-only metadata. No historical identity is inferred from timing or model.
CREATE TABLE IF NOT EXISTS fork_usage_execution_identity (
    usage_id BIGINT PRIMARY KEY REFERENCES usage_logs(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    thread_id VARCHAR(255) NOT NULL DEFAULT '',
    request_kind VARCHAR(255) NOT NULL DEFAULT '',
    subagent VARCHAR(255) NOT NULL DEFAULT '',
    source VARCHAR(40) NOT NULL,
    scope_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_fork_usage_execution_scope
    ON fork_usage_execution_identity(user_id, api_key_id, scope_key, created_at DESC);
