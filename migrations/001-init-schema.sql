-- Migration: 001_init_schema.sql
-- Criado: 2026-04-25
-- Descrição: Schema inicial para IDP Platform

-- ============ Users Table ============
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Basic info
    email VARCHAR(255) UNIQUE NOT NULL,
    full_name VARCHAR(255) NOT NULL,
    avatar TEXT,
    role VARCHAR(50) NOT NULL DEFAULT 'developer' 
        CHECK (role IN ('admin', 'maintainer', 'developer', 'viewer')),
    
    -- OAuth integration
    github_id VARCHAR(255),
    gitlab_id VARCHAR(255),
    github_token TEXT,
    gitlab_token TEXT,
    
    -- Status
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_seen TIMESTAMP,
    
    -- Audit
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    
    -- Indexes
    CONSTRAINT email_unique UNIQUE (email),
    CONSTRAINT github_id_unique UNIQUE (github_id),
    CONSTRAINT gitlab_id_unique UNIQUE (gitlab_id)
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_github_id ON users(github_id);
CREATE INDEX IF NOT EXISTS idx_users_gitlab_id ON users(gitlab_id);
CREATE INDEX IF NOT EXISTS idx_users_is_active ON users(is_active);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users(deleted_at);

-- ============ Repositories Table ============
CREATE TABLE IF NOT EXISTS repositories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Basic info
    name VARCHAR(255) NOT NULL,
    description TEXT,
    url TEXT NOT NULL UNIQUE,
    type VARCHAR(50) NOT NULL 
        CHECK (type IN ('github', 'gitlab', 'gitea', 'custom')),
    
    -- Ownership
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    is_public BOOLEAN NOT NULL DEFAULT false,
    
    -- Metadata (JSONB)
    -- Contains: owner_id, provider_id, webhook_id, languages, frameworks, stars, etc
    metadata JSONB NOT NULL DEFAULT '{}',
    
    -- Analysis tracking
    last_analyzed_at TIMESTAMP,
    analysis_status VARCHAR(50) NOT NULL DEFAULT 'pending'
        CHECK (analysis_status IN ('pending', 'in_progress', 'completed', 'failed')),
    analysis_error TEXT,
    
    -- AI Review tracking
    last_reviewed_at TIMESTAMP,
    reviews_count INT NOT NULL DEFAULT 0,
    
    -- Sync status
    last_synced_at TIMESTAMP,
    sync_status VARCHAR(50) NOT NULL DEFAULT 'idle'
        CHECK (sync_status IN ('idle', 'syncing', 'error')),
    sync_error TEXT,
    
    -- Audit
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    
    -- Constraints
    CONSTRAINT url_unique UNIQUE (url)
);

CREATE INDEX IF NOT EXISTS idx_repositories_owner_user_id ON repositories(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_repositories_type ON repositories(type);
CREATE INDEX IF NOT EXISTS idx_repositories_url ON repositories(url);
CREATE INDEX IF NOT EXISTS idx_repositories_analysis_status ON repositories(analysis_status);
CREATE INDEX IF NOT EXISTS idx_repositories_sync_status ON repositories(sync_status);
CREATE INDEX IF NOT EXISTS idx_repositories_last_analyzed_at ON repositories(last_analyzed_at);
CREATE INDEX IF NOT EXISTS idx_repositories_deleted_at ON repositories(deleted_at);

-- JSONB index for metadata search
CREATE INDEX IF NOT EXISTS idx_repositories_metadata ON repositories USING GIN(metadata);

-- ============ Repository Dependencies ============
CREATE TABLE IF NOT EXISTS repository_dependencies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    depends_on_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    
    -- Metadata
    type VARCHAR(50) NOT NULL DEFAULT 'import'  -- import, library, service, etc
        CHECK (type IN ('import', 'library', 'service', 'api', 'database', 'cache', 'other')),
    is_optional BOOLEAN NOT NULL DEFAULT false,
    version VARCHAR(255),
    
    -- Audit
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints
    CONSTRAINT no_self_dependency CHECK (repository_id != depends_on_id),
    CONSTRAINT unique_dependency UNIQUE (repository_id, depends_on_id)
);

CREATE INDEX IF NOT EXISTS idx_repo_dependencies_repository_id ON repository_dependencies(repository_id);
CREATE INDEX IF NOT EXISTS idx_repo_dependencies_depends_on_id ON repository_dependencies(depends_on_id);
CREATE INDEX IF NOT EXISTS idx_repo_dependencies_type ON repository_dependencies(type);

-- ============ User-Repository Association (Many-to-Many) ============
CREATE TABLE IF NOT EXISTS user_repositories (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    
    -- Permission level
    access_level VARCHAR(50) NOT NULL DEFAULT 'read'
        CHECK (access_level IN ('read', 'write', 'maintain', 'admin')),
    
    -- Audit
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    PRIMARY KEY (user_id, repository_id)
);

CREATE INDEX IF NOT EXISTS idx_user_repositories_user_id ON user_repositories(user_id);
CREATE INDEX IF NOT EXISTS idx_user_repositories_repository_id ON user_repositories(repository_id);

-- ============ Webhooks Table ============
CREATE TABLE IF NOT EXISTS webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    
    -- Event info
    event_type VARCHAR(50) NOT NULL
        CHECK (event_type IN ('push', 'pull_request', 'merge_request', 'issues', 
                             'release', 'repository', 'workflow_run', 'pipeline', 
                             'tag_push', 'unknown')),
    
    -- Event payload (JSONB)
    -- Contains: event_id, provider, branch, commit_sha, actor_name, raw_data, etc
    event_payload JSONB NOT NULL DEFAULT '{}',
    
    -- Processing status
    status VARCHAR(50) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    processing_error TEXT,
    
    -- Processing result (JSONB)
    -- Contains: success, processed_at, analysis_id, processing_time_ms, tokens_used
    processing_result JSONB,
    
    -- Delivery tracking
    delivery_id VARCHAR(255) NOT NULL UNIQUE,  -- GitHub/GitLab delivery ID
    retry_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 3,
    next_retry_at TIMESTAMP,
    failed_at TIMESTAMP,
    
    -- Audit
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    CONSTRAINT delivery_id_unique UNIQUE (delivery_id)
);

CREATE INDEX IF NOT EXISTS idx_webhooks_repository_id ON webhooks(repository_id);
CREATE INDEX IF NOT EXISTS idx_webhooks_event_type ON webhooks(event_type);
CREATE INDEX IF NOT EXISTS idx_webhooks_status ON webhooks(status);
CREATE INDEX IF NOT EXISTS idx_webhooks_delivery_id ON webhooks(delivery_id);
CREATE INDEX IF NOT EXISTS idx_webhooks_created_at ON webhooks(created_at);
CREATE INDEX IF NOT EXISTS idx_webhooks_next_retry_at ON webhooks(next_retry_at);

-- JSONB index for event search
CREATE INDEX IF NOT EXISTS idx_webhooks_event_payload ON webhooks USING GIN(event_payload);

-- ============ Webhook Configs Table ============
CREATE TABLE IF NOT EXISTS webhook_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL UNIQUE REFERENCES repositories(id) ON DELETE CASCADE,
    
    -- Configuration
    webhook_url TEXT NOT NULL,
    secret TEXT NOT NULL,
    events TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],  -- Array of event types
    is_active BOOLEAN NOT NULL DEFAULT true,
    
    -- Provider
    provider_webhook_id VARCHAR(255),
    provider_type VARCHAR(50) NOT NULL
        CHECK (provider_type IN ('github', 'gitlab', 'gitea')),
    
    -- Metrics
    last_delivery_at TIMESTAMP,
    successful_count INT NOT NULL DEFAULT 0,
    failed_count INT NOT NULL DEFAULT 0,
    
    -- Audit
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_webhook_configs_repository_id ON webhook_configs(repository_id);
CREATE INDEX IF NOT EXISTS idx_webhook_configs_provider_type ON webhook_configs(provider_type);
CREATE INDEX IF NOT EXISTS idx_webhook_configs_is_active ON webhook_configs(is_active);

-- ============ Code Analyses Table ============
CREATE TABLE IF NOT EXISTS code_analyses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    
    -- Analysis metadata
    type VARCHAR(50) NOT NULL
        CHECK (type IN ('code_review', 'metrics', 'dependency', 'security', 'architecture')),
    status VARCHAR(50) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'completed', 'failed', 'partial')),
    title VARCHAR(500) NOT NULL,
    description TEXT,
    
    -- Scope
    commit_sha VARCHAR(255),
    branch VARCHAR(255),
    pull_request_id INT,
    file_path VARCHAR(1000),
    
    -- Results (JSONB)
    -- issues: array of CodeIssue objects
    issues JSONB NOT NULL DEFAULT '[]',
    
    -- metrics: CodeMetrics object
    metrics JSONB,
    
    -- Summary
    summary_text TEXT,
    
    -- Issue counters
    issue_count INT NOT NULL DEFAULT 0,
    critical_count INT NOT NULL DEFAULT 0,
    error_count INT NOT NULL DEFAULT 0,
    warning_count INT NOT NULL DEFAULT 0,
    info_count INT NOT NULL DEFAULT 0,
    
    -- Triggered by
    triggered_by VARCHAR(255),  -- 'user', 'webhook', 'schedule'
    triggered_by_id UUID,
    
    -- AI metadata
    is_ai_analysis BOOLEAN NOT NULL DEFAULT true,
    ai_model VARCHAR(100),
    tokens_used INT,
    processing_ms BIGINT,
    
    -- Error tracking
    error_message TEXT,
    
    -- Audit
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    
    -- Relationships
    embedding_id UUID
);

CREATE INDEX IF NOT EXISTS idx_code_analyses_repository_id ON code_analyses(repository_id);
CREATE INDEX IF NOT EXISTS idx_code_analyses_type ON code_analyses(type);
CREATE INDEX IF NOT EXISTS idx_code_analyses_status ON code_analyses(status);
CREATE INDEX IF NOT EXISTS idx_code_analyses_commit_sha ON code_analyses(commit_sha);
CREATE INDEX IF NOT EXISTS idx_code_analyses_pull_request_id ON code_analyses(pull_request_id);
CREATE INDEX IF NOT EXISTS idx_code_analyses_issue_count ON code_analyses(issue_count);
CREATE INDEX IF NOT EXISTS idx_code_analyses_created_at ON code_analyses(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_code_analyses_deleted_at ON code_analyses(deleted_at);

-- JSONB index for issues search
CREATE INDEX IF NOT EXISTS idx_code_analyses_issues ON code_analyses USING GIN(issues);

-- ============ Code Embeddings Table (for Semantic Search) ============
CREATE TABLE IF NOT EXISTS code_embeddings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    
    -- Content
    file_path VARCHAR(1000) NOT NULL,
    content TEXT NOT NULL,
    content_hash VARCHAR(255) NOT NULL,  -- SHA256 for deduplication
    description TEXT,
    language VARCHAR(50),
    start_line INT,
    end_line INT,
    
    -- Vector embedding (pgvector)
    embedding VECTOR(1536),  -- OpenAI embeddings dimension
    
    -- Metadata
    tokens INT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints
    CONSTRAINT unique_embedding_per_repo UNIQUE (repository_id, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_code_embeddings_repository_id ON code_embeddings(repository_id);
CREATE INDEX IF NOT EXISTS idx_code_embeddings_file_path ON code_embeddings(file_path);
CREATE INDEX IF NOT EXISTS idx_code_embeddings_language ON code_embeddings(language);
CREATE INDEX IF NOT EXISTS idx_code_embeddings_content_hash ON code_embeddings(content_hash);

-- Vector index for semantic search (IVFFlat is good for many vectors)
-- Alternative: HNSW for larger datasets, use "ivfflat" for medium (0-1M vectors)
CREATE INDEX IF NOT EXISTS idx_code_embeddings_vector ON code_embeddings USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

-- ============ Functions for Audit Timestamps ============
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create triggers for auto-updating updated_at
DROP TRIGGER IF EXISTS users_updated_at_trigger ON users;
CREATE TRIGGER users_updated_at_trigger
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS repositories_updated_at_trigger ON repositories;
CREATE TRIGGER repositories_updated_at_trigger
    BEFORE UPDATE ON repositories
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS webhooks_updated_at_trigger ON webhooks;
CREATE TRIGGER webhooks_updated_at_trigger
    BEFORE UPDATE ON webhooks
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS webhook_configs_updated_at_trigger ON webhook_configs;
CREATE TRIGGER webhook_configs_updated_at_trigger
    BEFORE UPDATE ON webhook_configs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS code_analyses_updated_at_trigger ON code_analyses;
CREATE TRIGGER code_analyses_updated_at_trigger
    BEFORE UPDATE ON code_analyses
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS code_embeddings_updated_at_trigger ON code_embeddings;
CREATE TRIGGER code_embeddings_updated_at_trigger
    BEFORE UPDATE ON code_embeddings
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============ Summary ============
-- Tables created:
-- 1. users - Platform users with roles and OAuth
-- 2. repositories - Git repositories from multiple sources
-- 3. repository_dependencies - Dependency graph between repos
-- 4. user_repositories - Many-to-many association with access levels
-- 5. webhooks - Incoming events from GitHub/GitLab/Gitea
-- 6. webhook_configs - Webhook configuration per repository
-- 7. code_analyses - Code review and metric analysis results
-- 8. code_embeddings - Semantic embeddings for vector search
--
-- Features:
-- - Soft deletes (deleted_at column)
-- - JSONB for flexible metadata
-- - pgvector for semantic search
-- - Indexes for query performance
-- - Cascading deletes for referential integrity
-- - Audit triggers for updated_at
-- - Role-based access control (RBAC) foundation