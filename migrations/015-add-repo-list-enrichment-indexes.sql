-- Migration 015: Add indexes for enriched repository list query
-- Optimizes LATERAL joins for code_analyses aggregation

-- Composite index for LATERAL join filtering by repo + type + ordering by created_at DESC
CREATE INDEX IF NOT EXISTS idx_code_analyses_repo_type_created
    ON code_analyses (repository_id, type, created_at DESC)
    WHERE deleted_at IS NULL AND status = 'completed';

-- Composite index for main query filtering + ordering
CREATE INDEX IF NOT EXISTS idx_repositories_org_created
    ON repositories (organization_id, created_at DESC)
    WHERE deleted_at IS NULL;
