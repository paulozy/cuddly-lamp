CREATE TABLE IF NOT EXISTS coverage_upload_tokens (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id        UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name                 VARCHAR(255) NOT NULL,
    token_hash           VARCHAR(64) NOT NULL UNIQUE,
    created_by_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    last_used_at         TIMESTAMP,
    expires_at           TIMESTAMP,
    revoked_at           TIMESTAMP,
    created_at           TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE INDEX IF NOT EXISTS idx_coverage_upload_tokens_repo
    ON coverage_upload_tokens (repository_id)
    WHERE revoked_at IS NULL;
