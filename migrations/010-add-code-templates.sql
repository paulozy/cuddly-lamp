CREATE TABLE IF NOT EXISTS code_templates (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    repository_id       UUID REFERENCES repositories(id) ON DELETE SET NULL,
    created_by_user_id  UUID REFERENCES users(id) ON DELETE SET NULL,
    prompt              TEXT NOT NULL,
    stack_hint          TEXT,
    stack_snapshot      JSONB NOT NULL DEFAULT '{}',
    status              VARCHAR(50) NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','generating','completed','failed')),
    summary             TEXT,
    files               JSONB NOT NULL DEFAULT '[]',
    ai_model            VARCHAR(100),
    tokens_used         INT NOT NULL DEFAULT 0,
    processing_ms       BIGINT NOT NULL DEFAULT 0,
    error_message       TEXT,
    is_pinned           BOOLEAN NOT NULL DEFAULT false,
    pinned_by_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    pinned_at           TIMESTAMP,
    name                VARCHAR(255),
    created_at          TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at          TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    deleted_at          TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_code_templates_organization_id ON code_templates(organization_id);
CREATE INDEX IF NOT EXISTS idx_code_templates_repository_id  ON code_templates(repository_id);
CREATE INDEX IF NOT EXISTS idx_code_templates_status         ON code_templates(status);
CREATE INDEX IF NOT EXISTS idx_code_templates_is_pinned      ON code_templates(is_pinned) WHERE is_pinned = true;
CREATE INDEX IF NOT EXISTS idx_code_templates_deleted_at     ON code_templates(deleted_at);
