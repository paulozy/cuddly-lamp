-- Align semantic-search storage with Voyage code embeddings.

ALTER TABLE code_embeddings
    ADD COLUMN IF NOT EXISTS provider VARCHAR(50) NOT NULL DEFAULT 'voyage',
    ADD COLUMN IF NOT EXISTS model VARCHAR(100) NOT NULL DEFAULT 'voyage-code-3',
    ADD COLUMN IF NOT EXISTS dimension INT NOT NULL DEFAULT 1024,
    ADD COLUMN IF NOT EXISTS branch VARCHAR(255),
    ADD COLUMN IF NOT EXISTS commit_sha VARCHAR(100);

DROP INDEX IF EXISTS idx_code_embeddings_vector;
DROP INDEX IF EXISTS unique_embedding_per_repo_provider_idx;
ALTER TABLE code_embeddings DROP CONSTRAINT IF EXISTS unique_embedding_per_repo;

ALTER TABLE code_embeddings
    ALTER COLUMN embedding TYPE VECTOR(1024) USING NULL;

CREATE UNIQUE INDEX IF NOT EXISTS unique_embedding_per_repo_provider_idx
    ON code_embeddings(repository_id, provider, model, dimension, COALESCE(branch, ''), content_hash);

CREATE INDEX IF NOT EXISTS idx_code_embeddings_provider_model
    ON code_embeddings(provider, model, dimension);

CREATE INDEX IF NOT EXISTS idx_code_embeddings_branch
    ON code_embeddings(branch);

CREATE INDEX IF NOT EXISTS idx_code_embeddings_vector
    ON code_embeddings USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
