-- 021 — Expose embeddings pipeline state on the repositories row.
--
-- The embedding worker today writes only to `code_embeddings`; the frontend
-- has no way of knowing whether a repo is indexed, indexing, stale or never
-- indexed. We mirror the pattern of `sync_status` / `analysis_status` and add
-- a small surface on the repositories table so listing endpoints can join
-- with no extra LATERAL.
--
-- Status lifecycle:
--   idle      → never indexed (or provider unavailable)
--   pending   → enqueued, worker has not started
--   indexing  → worker is processing batches
--   indexed   → successfully indexed
--   stale     → indexed but a push has changed code since (sinaliza re-index)
--   failed    → worker errored; `embeddings_error` carries the message
--
-- `embeddings_count` is the running count of chunks; the worker increments
-- it batch by batch so the UI can show partial progress while indexing.

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS embeddings_status     VARCHAR(20) NOT NULL DEFAULT 'idle',
    ADD COLUMN IF NOT EXISTS embeddings_count      INT          NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS embeddings_indexed_at TIMESTAMP    NULL,
    ADD COLUMN IF NOT EXISTS embeddings_error      TEXT         NULL;

ALTER TABLE repositories
    ADD CONSTRAINT repositories_embeddings_status_check CHECK (
        embeddings_status IN ('idle', 'pending', 'indexing', 'indexed', 'stale', 'failed')
    );
