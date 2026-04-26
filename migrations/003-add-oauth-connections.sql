-- Migration 003: Add oauth_connections table and migrate existing OAuth data

-- 1. Create oauth_connections table
CREATE TABLE IF NOT EXISTS oauth_connections (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         VARCHAR(50) NOT NULL,
    provider_user_id VARCHAR(255) NOT NULL,
    access_token     TEXT,
    created_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT oauth_connections_unique UNIQUE (provider, provider_user_id)
);

CREATE INDEX IF NOT EXISTS idx_oauth_connections_user_id ON oauth_connections(user_id);

-- 2. Migrate existing GitHub data (if columns exist)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'github_id') THEN
        INSERT INTO oauth_connections (user_id, provider, provider_user_id, access_token, created_at, updated_at)
            SELECT id, 'github', github_id, github_token, NOW(), NOW()
            FROM users
            WHERE github_id IS NOT NULL AND github_id != ''
            ON CONFLICT (provider, provider_user_id) DO NOTHING;
    END IF;
END $$;

-- 3. Migrate existing GitLab data (if columns exist)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'gitlab_id') THEN
        INSERT INTO oauth_connections (user_id, provider, provider_user_id, access_token, created_at, updated_at)
            SELECT id, 'gitlab', gitlab_id, gitlab_token, NOW(), NOW()
            FROM users
            WHERE gitlab_id IS NOT NULL AND gitlab_id != ''
            ON CONFLICT (provider, provider_user_id) DO NOTHING;
    END IF;
END $$;

-- 4. Drop old OAuth columns from users table (if they exist)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'github_id') THEN
        DROP INDEX IF EXISTS idx_users_github_id;
        DROP INDEX IF EXISTS idx_users_gitlab_id;

        ALTER TABLE users
            DROP CONSTRAINT IF EXISTS github_id_unique,
            DROP CONSTRAINT IF EXISTS gitlab_id_unique,
            DROP COLUMN IF EXISTS github_id,
            DROP COLUMN IF EXISTS gitlab_id,
            DROP COLUMN IF EXISTS github_token,
            DROP COLUMN IF EXISTS gitlab_token;
    END IF;
END $$;
