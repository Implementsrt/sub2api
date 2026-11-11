-- Fork-only side tables; local billing and generated Ent schema stay unchanged.
CREATE TABLE IF NOT EXISTS fork_upstream_pricing_sources (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT 0,
    config JSONB NOT NULL,
    credential TEXT NOT NULL,
    account_fingerprint TEXT NOT NULL,
    active_version BIGINT,
    lease_token TEXT,
    lease_until TIMESTAMPTZ,
    last_success TIMESTAMPTZ,
    last_attempt TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    next_sync TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS fork_upstream_pricing_versions (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    config JSONB NOT NULL,
    account_fingerprint TEXT NOT NULL,
    catalog JSONB NOT NULL,
    fingerprint TEXT NOT NULL,
    effective_from TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    effective_until TIMESTAMPTZ,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS idx_fork_upstream_pricing_version_time
    ON fork_upstream_pricing_versions(account_id, effective_from DESC);
CREATE TABLE IF NOT EXISTS fork_upstream_pricing_previews (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    token TEXT NOT NULL,
    revision BIGINT NOT NULL,
    config JSONB NOT NULL,
    credential TEXT NOT NULL,
    account_fingerprint TEXT NOT NULL,
    catalog JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS fork_upstream_pricing_runs (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    trigger TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    version_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS idx_fork_upstream_pricing_runs_account
    ON fork_upstream_pricing_runs(account_id, id DESC);
CREATE TABLE IF NOT EXISTS fork_upstream_pricing_usage (
    usage_id BIGINT PRIMARY KEY REFERENCES usage_logs(id) ON DELETE CASCADE,
    version_id BIGINT REFERENCES fork_upstream_pricing_versions(id),
    payload JSONB NOT NULL,
    estimate JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS idx_fork_upstream_pricing_pending
    ON fork_upstream_pricing_usage(usage_id) WHERE estimate IS NULL;
