-- 019 — Extend doc_generations with org-wide scope.
--
-- Adds `scope`, `organization_id`, `template_id`, `progress_stage`, and
-- `superseded_by_id` to support org-level documentation generation alongside
-- the existing per-repo flow.
--
-- Strategy: existing rows are all per-repo with `organization_id` derivable
-- from the linked repository, so the new column is backfilled before being
-- enforced NOT NULL.

-- 1. Add new columns nullable first so the table can be altered without
-- requiring an immediate value for existing rows.
ALTER TABLE doc_generations
    ADD COLUMN IF NOT EXISTS scope            VARCHAR(8)  NOT NULL DEFAULT 'repo',
    ADD COLUMN IF NOT EXISTS organization_id  UUID,
    ADD COLUMN IF NOT EXISTS template_id      VARCHAR(64),
    ADD COLUMN IF NOT EXISTS progress_stage   VARCHAR(48),
    ADD COLUMN IF NOT EXISTS superseded_by_id UUID,
    ADD COLUMN IF NOT EXISTS user_prompt      TEXT;

-- 2. Backfill organization_id from the repositories table for legacy rows.
UPDATE doc_generations dg
SET organization_id = r.organization_id
FROM repositories r
WHERE dg.repository_id = r.id
  AND dg.organization_id IS NULL;

-- 3. Enforce non-null on organization_id now that legacy rows are populated.
ALTER TABLE doc_generations
    ALTER COLUMN organization_id SET NOT NULL;

-- 4. Drop the legacy NOT NULL constraint on repository_id — org-level rows
--    do not belong to any specific repo.
ALTER TABLE doc_generations
    ALTER COLUMN repository_id DROP NOT NULL;

-- 5. Foreign keys for the new columns.
ALTER TABLE doc_generations
    ADD CONSTRAINT doc_generations_organization_id_fkey
        FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE,
    ADD CONSTRAINT doc_generations_superseded_by_id_fkey
        FOREIGN KEY (superseded_by_id) REFERENCES doc_generations(id) ON DELETE SET NULL;

-- 6. Mutual-exclusion check: a row is either per-repo (with repository_id) or
--    org-level (with no repository_id).
ALTER TABLE doc_generations
    ADD CONSTRAINT doc_generations_scope_check CHECK (
        (scope = 'repo' AND repository_id IS NOT NULL) OR
        (scope = 'org'  AND repository_id IS NULL)
    );

-- 7. Index that supports the org-level listing endpoint and AnalysisWorker's
--    cross-reference lookup (org docs joined into per-repo analysis prompts).
CREATE INDEX IF NOT EXISTS idx_doc_generations_org_latest_completed
    ON doc_generations(organization_id, scope, created_at DESC)
    WHERE status = 'completed' AND deleted_at IS NULL AND superseded_by_id IS NULL;
