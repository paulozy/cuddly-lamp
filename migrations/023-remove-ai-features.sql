-- 023 — Drop the schema behind the removed AI features.
--
-- The platform is now a plain IDP: repository catalog, pull request browsing,
-- relationship graph, CI coverage, and documentation. Documentation generation
-- is the one LLM-backed feature that remains, so `doc_generations` and the
-- Anthropic columns on `organization_configs` are deliberately kept.
--
-- What goes:
--   * code_analyses    — AI code review / PR findings
--   * code_templates   — AI code scaffolding
--   * code_embeddings  — Voyage vectors backing semantic search
--   * repositories.analysis_* / reviews_count / last_analyzed_at / last_reviewed_at
--   * repositories.embeddings_*
--   * organization_configs Voyage + PR-review-posting columns
--
-- THIS IS DESTRUCTIVE AND IRREVERSIBLE: the dropped tables hold generated
-- review findings, templates and vectors. Take a dump first if any of it is
-- worth keeping.
--
-- All steps are idempotent.

-- 1. Drop the AI tables. CASCADE clears dependent indexes/constraints.
DROP TABLE IF EXISTS code_embeddings CASCADE;
DROP TABLE IF EXISTS code_templates CASCADE;
DROP TABLE IF EXISTS code_analyses CASCADE;

-- 2. Drop the CHECK constraints before their columns (Postgres would do this
--    with the column, but being explicit keeps the intent readable).
ALTER TABLE repositories
    DROP CONSTRAINT IF EXISTS repositories_embeddings_status_check,
    DROP CONSTRAINT IF EXISTS repositories_analysis_status_check;

-- 3. Drop the analysis + embeddings surface from repositories.
ALTER TABLE repositories
    DROP COLUMN IF EXISTS analysis_status,
    DROP COLUMN IF EXISTS analysis_error,
    DROP COLUMN IF EXISTS last_analyzed_at,
    DROP COLUMN IF EXISTS last_reviewed_at,
    DROP COLUMN IF EXISTS reviews_count,
    DROP COLUMN IF EXISTS embeddings_status,
    DROP COLUMN IF EXISTS embeddings_count,
    DROP COLUMN IF EXISTS embeddings_indexed_at,
    DROP COLUMN IF EXISTS embeddings_error;

-- 4. Drop the embeddings provider config and the GitHub PR-review toggle.
--    Anthropic key/budget columns stay — documentation generation uses them.
ALTER TABLE organization_configs
    DROP COLUMN IF EXISTS embeddings_provider,
    DROP COLUMN IF EXISTS voyage_api_key,
    DROP COLUMN IF EXISTS embeddings_model,
    DROP COLUMN IF EXISTS embeddings_dimensions,
    DROP COLUMN IF EXISTS github_pr_review_enabled;

-- 5. The pgvector extension backed code_embeddings only. Dropping it is safe
--    once that table is gone, but we leave it installed: it is harmless, and
--    dropping an extension another database object might reference is a
--    worse failure mode than an unused one.
