-- Migration 008: organization-scoped multitenancy.

CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(120) NOT NULL UNIQUE,
    description TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_organizations_slug ON organizations(slug);
CREATE INDEX IF NOT EXISTS idx_organizations_deleted_at ON organizations(deleted_at);

CREATE TABLE IF NOT EXISTS organization_members (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL DEFAULT 'developer'
        CHECK (role IN ('admin', 'maintainer', 'developer', 'viewer')),
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (organization_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_org_members_user_id ON organization_members(user_id);
CREATE INDEX IF NOT EXISTS idx_org_members_role ON organization_members(role);

CREATE TABLE IF NOT EXISTS organization_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL UNIQUE REFERENCES organizations(id) ON DELETE CASCADE,

    anthropic_api_key BYTEA,
    anthropic_tokens_per_hour INT NOT NULL DEFAULT 20000,
    github_token BYTEA,
    github_pr_review_enabled BOOLEAN NOT NULL DEFAULT false,
    webhook_base_url TEXT,

    embeddings_provider VARCHAR(50) NOT NULL DEFAULT 'voyage',
    voyage_api_key BYTEA,
    embeddings_model VARCHAR(100) NOT NULL DEFAULT 'voyage-code-3',
    embeddings_dimensions INT NOT NULL DEFAULT 1024,

    github_client_id VARCHAR(255),
    github_client_secret BYTEA,
    github_callback_url TEXT,
    gitlab_client_id VARCHAR(255),
    gitlab_client_secret BYTEA,
    gitlab_callback_url TEXT,

    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_org_configs_organization_id ON organization_configs(organization_id);

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE repositories
    ALTER COLUMN owner_user_id DROP NOT NULL;

UPDATE repositories
SET created_by_user_id = owner_user_id
WHERE created_by_user_id IS NULL;

DROP INDEX IF EXISTS idx_repositories_organization_id;
CREATE INDEX IF NOT EXISTS idx_repositories_organization_id ON repositories(organization_id);
CREATE INDEX IF NOT EXISTS idx_repositories_created_by_user_id ON repositories(created_by_user_id);

ALTER TABLE repositories DROP CONSTRAINT IF EXISTS url_unique;
DROP INDEX IF EXISTS idx_repositories_url;
CREATE UNIQUE INDEX IF NOT EXISTS idx_repositories_org_url_unique
    ON repositories(organization_id, url)
    WHERE deleted_at IS NULL;

ALTER TABLE tokens
    ADD COLUMN IF NOT EXISTS organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_tokens_organization_id ON tokens(organization_id);

DROP TRIGGER IF EXISTS organizations_updated_at_trigger ON organizations;
CREATE TRIGGER organizations_updated_at_trigger
    BEFORE UPDATE ON organizations
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS organization_members_updated_at_trigger ON organization_members;
CREATE TRIGGER organization_members_updated_at_trigger
    BEFORE UPDATE ON organization_members
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS organization_configs_updated_at_trigger ON organization_configs;
CREATE TRIGGER organization_configs_updated_at_trigger
    BEFORE UPDATE ON organization_configs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
