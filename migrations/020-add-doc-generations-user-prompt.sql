-- 020 — Add user_prompt column to doc_generations.
--
-- The org-wide doc generation flow accepts a free-text topic from the user
-- (e.g. "Should we standardize on PostgreSQL?") which is stored on the row
-- so the listing UI can show what was requested and so we can re-run the
-- same prompt if needed. Migration 019 introduced the model field but
-- forgot the actual column — this corrective migration adds it.
--
-- Idempotent (`IF NOT EXISTS`) so it's safe to re-apply.

ALTER TABLE doc_generations
    ADD COLUMN IF NOT EXISTS user_prompt TEXT;
