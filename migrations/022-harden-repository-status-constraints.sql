-- 022 — Harden the three status columns of `repositories` with backfill +
-- CHECK constraints. Pairs with the postgres_repository.go SELECT fix in
-- the same branch: the backfill catches any rows that already got '' written
-- between the 021 deploy and the SELECT fix; the new CHECKs bring
-- `sync_status` and `analysis_status` to parity with `embeddings_status`.
--
-- All steps are idempotent.

-- 1. Backfill rows that ended up with NULL or '' on embeddings_status. Only
--    possible on rows touched between migration 021 and this fix.
UPDATE repositories
SET    embeddings_status = 'idle'
WHERE  embeddings_status IS NULL OR embeddings_status = '';

-- 2. Defensive backfill for sync_status / analysis_status. Should be a no-op
--    on a healthy DB but it's free insurance before adding CHECKs.
UPDATE repositories
SET    sync_status = 'idle'
WHERE  sync_status IS NULL OR sync_status = '';

UPDATE repositories
SET    analysis_status = 'pending'
WHERE  analysis_status IS NULL OR analysis_status = '';

-- 3. Add CHECK constraints to sync_status and analysis_status. These mirror
--    the protection embeddings_status already received in migration 021.
--    Postgres has no `ADD CONSTRAINT IF NOT EXISTS`, so we guard with a
--    catalog lookup to keep the migration re-runnable.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'repositories_sync_status_check'
    ) THEN
        ALTER TABLE repositories
            ADD CONSTRAINT repositories_sync_status_check CHECK (
                sync_status IN ('idle', 'syncing', 'synced', 'error')
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'repositories_analysis_status_check'
    ) THEN
        ALTER TABLE repositories
            ADD CONSTRAINT repositories_analysis_status_check CHECK (
                analysis_status IN ('pending', 'in_progress', 'completed', 'failed')
            );
    END IF;
END$$;
