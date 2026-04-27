ALTER TABLE repositories
    DROP CONSTRAINT IF EXISTS repositories_sync_status_check;

ALTER TABLE repositories
    ADD CONSTRAINT repositories_sync_status_check
        CHECK (sync_status IN ('idle', 'syncing', 'synced', 'error'));
