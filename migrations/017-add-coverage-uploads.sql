CREATE TABLE IF NOT EXISTS coverage_uploads (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id        UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    commit_sha           VARCHAR(64) NOT NULL,
    branch               VARCHAR(255),
    format               VARCHAR(32) NOT NULL,
    lines_covered        INT NOT NULL DEFAULT 0,
    lines_total          INT NOT NULL DEFAULT 0,
    percentage           DOUBLE PRECISION NOT NULL DEFAULT 0,
    status               VARCHAR(32) NOT NULL,
    raw_size_bytes       INT NOT NULL DEFAULT 0,
    files                JSONB NOT NULL DEFAULT '{}'::jsonb,
    warnings             TEXT[] NOT NULL DEFAULT '{}',
    uploaded_by_token_id UUID,
    created_at           TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE INDEX IF NOT EXISTS idx_coverage_uploads_repo_sha
    ON coverage_uploads (repository_id, commit_sha, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_coverage_uploads_repo_created
    ON coverage_uploads (repository_id, created_at DESC);
