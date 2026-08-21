-- 027 — Index the two signals the scorecard adds to the enriched repository
-- query.
--
-- The scorecard evaluates its checks in Go from a single read; these back the
-- two EXISTS semi-joins that read supply. Postgres short-circuits a semi-join on
-- the first matching row, so a partial index covering exactly the predicate is
-- all that is needed.
--
-- `webhook_configs.repository_id` is already UNIQUE, which is its own index —
-- nothing to add there.

-- "does this repository have documentation?" — one row is enough, so the index
-- only needs to cover completed, non-deleted generations.
CREATE INDEX IF NOT EXISTS idx_doc_generations_repo_completed
    ON doc_generations (repository_id)
    WHERE status = 'completed' AND deleted_at IS NULL;
