-- Fork extension: bounded session predecessor lookup for cache-drop monitoring.
-- Distinct filename/index namespace avoids collisions with future upstream work.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_fork_usage_user_session_created
    ON usage_logs (user_id, session_id, created_at DESC)
    WHERE session_id IS NOT NULL;
