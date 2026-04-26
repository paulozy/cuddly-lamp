-- Users table (update existing)
ALTER TABLE users 
ADD COLUMN IF NOT EXISTS password_hash VARCHAR(255);

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);

-- User repositories (update existing)
ALTER TABLE user_repositories
DROP COLUMN IF EXISTS access_level,
ADD COLUMN IF NOT EXISTS access_level VARCHAR(50) NOT NULL DEFAULT 'viewer' CHECK (access_level IN ('viewer', 'developer', 'maintainer', 'admin'));

-- Tokens table (new)
CREATE TABLE IF NOT EXISTS tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    jti VARCHAR(255) UNIQUE NOT NULL,
    token_hash VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL DEFAULT 'access',
    is_revoked BOOLEAN DEFAULT false,
    revoke_reason VARCHAR(255),
    created_at TIMESTAMP DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP,
    last_used_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_tokens_jti ON tokens(jti);
CREATE INDEX IF NOT EXISTS idx_tokens_expires_at ON tokens(expires_at);
CREATE INDEX IF NOT EXISTS idx_tokens_is_revoked ON tokens(is_revoked);