CREATE TABLE IF NOT EXISTS repository_relationships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    source_repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    target_repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,

    kind VARCHAR(50) NOT NULL
        CHECK (kind IN ('http', 'async', 'library', 'data', 'infra', 'manual', 'other')),
    label VARCHAR(255),
    description TEXT,
    source VARCHAR(50) NOT NULL DEFAULT 'manual'
        CHECK (source IN ('manual', 'analysis', 'manifest', 'import', 'webhook', 'legacy_dependency')),
    confidence NUMERIC(5,4) NOT NULL DEFAULT 1.0
        CHECK (confidence >= 0 AND confidence <= 1),
    metadata JSONB NOT NULL DEFAULT '{}',

    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,

    CONSTRAINT no_self_repository_relationship CHECK (source_repository_id != target_repository_id)
);

CREATE INDEX IF NOT EXISTS idx_repo_relationships_org_id ON repository_relationships(organization_id);
CREATE INDEX IF NOT EXISTS idx_repo_relationships_source_repo_id ON repository_relationships(source_repository_id);
CREATE INDEX IF NOT EXISTS idx_repo_relationships_target_repo_id ON repository_relationships(target_repository_id);
CREATE INDEX IF NOT EXISTS idx_repo_relationships_kind ON repository_relationships(kind);
CREATE INDEX IF NOT EXISTS idx_repo_relationships_source ON repository_relationships(source);
CREATE INDEX IF NOT EXISTS idx_repo_relationships_deleted_at ON repository_relationships(deleted_at);
CREATE INDEX IF NOT EXISTS idx_repo_relationships_metadata ON repository_relationships USING GIN(metadata);

DROP TRIGGER IF EXISTS repository_relationships_updated_at_trigger ON repository_relationships;
CREATE TRIGGER repository_relationships_updated_at_trigger
    BEFORE UPDATE ON repository_relationships
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

INSERT INTO repository_relationships (
    organization_id,
    source_repository_id,
    target_repository_id,
    kind,
    label,
    source,
    confidence,
    metadata,
    created_at,
    updated_at
)
SELECT
    source_repo.organization_id,
    rd.repository_id,
    rd.depends_on_id,
    CASE
        WHEN rd.type IN ('import', 'library') THEN 'library'
        WHEN rd.type IN ('api', 'service') THEN 'http'
        WHEN rd.type IN ('database', 'cache') THEN 'data'
        ELSE 'other'
    END,
    rd.type,
    'legacy_dependency',
    1.0,
    jsonb_strip_nulls(jsonb_build_object(
        'legacy_dependency_id', rd.id,
        'legacy_type', rd.type,
        'is_optional', rd.is_optional,
        'version', rd.version
    )),
    rd.created_at,
    rd.updated_at
FROM repository_dependencies rd
JOIN repositories source_repo ON source_repo.id = rd.repository_id
JOIN repositories target_repo ON target_repo.id = rd.depends_on_id
WHERE source_repo.organization_id IS NOT NULL
  AND source_repo.organization_id = target_repo.organization_id
  AND NOT EXISTS (
      SELECT 1
      FROM repository_relationships rr
      WHERE rr.source_repository_id = rd.repository_id
        AND rr.target_repository_id = rd.depends_on_id
        AND rr.source = 'legacy_dependency'
        AND rr.metadata->>'legacy_dependency_id' = rd.id::text
  );
