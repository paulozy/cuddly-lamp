ALTER TABLE tokens
    ADD COLUMN IF NOT EXISTS family_id UUID,
    ADD COLUMN IF NOT EXISTS parent_jti VARCHAR(255);

CREATE INDEX IF NOT EXISTS idx_tokens_family_id ON tokens(family_id);
CREATE INDEX IF NOT EXISTS idx_tokens_type ON tokens(type);
