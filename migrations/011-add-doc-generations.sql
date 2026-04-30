CREATE TABLE IF NOT EXISTS doc_generations (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id         UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    status                VARCHAR(50) NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending','in_progress','completed','failed')),
    types                 JSONB NOT NULL DEFAULT '[]',
    branch                VARCHAR(255),
    gen_branch            VARCHAR(255),
    pull_request_url      TEXT,
    pull_request_number   INT NOT NULL DEFAULT 0,
    content               JSONB NOT NULL DEFAULT '{}',
    tokens_used           INT NOT NULL DEFAULT 0,
    error_message         TEXT,
    triggered_by_user_id  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at            TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at            TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    deleted_at            TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_doc_generations_repository_id ON doc_generations(repository_id);
CREATE INDEX IF NOT EXISTS idx_doc_generations_status        ON doc_generations(status);
CREATE INDEX IF NOT EXISTS idx_doc_generations_deleted_at    ON doc_generations(deleted_at);
CREATE INDEX IF NOT EXISTS idx_doc_generations_latest_completed
    ON doc_generations(repository_id, created_at DESC)
    WHERE status = 'completed' AND deleted_at IS NULL;
