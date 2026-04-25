-- 001_init_schema.sql
-- Initial schema with core entities

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'member', -- admin, member, viewer
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_deleted_at ON users(deleted_at);

-- Repositories table
CREATE TABLE IF NOT EXISTS repositories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    url VARCHAR(500) NOT NULL UNIQUE,
    description TEXT,
    type VARCHAR(50) NOT NULL, -- github, gitlab, gitea
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    
    -- Sync and analysis status
    last_synced_at TIMESTAMP,
    sync_status VARCHAR(50) DEFAULT 'pending', -- pending, syncing, success, failed
    analysis_status VARCHAR(50) DEFAULT 'pending', -- pending, analyzing, success, failed
    
    -- Metadata
    language VARCHAR(50),
    branch_count INT DEFAULT 0,
    commit_count INT DEFAULT 0,
    stars INT DEFAULT 0,
    
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    
    CONSTRAINT fk_repositories_owner FOREIGN KEY (owner_id) REFERENCES users(id)
);

CREATE INDEX idx_repositories_type ON repositories(type);
CREATE INDEX idx_repositories_owner_id ON repositories(owner_id);
CREATE INDEX idx_repositories_sync_status ON repositories(sync_status);
CREATE INDEX idx_repositories_analysis_status ON repositories(analysis_status);
CREATE INDEX idx_repositories_deleted_at ON repositories(deleted_at);

-- Webhooks table
CREATE TABLE IF NOT EXISTS webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL, -- push, pull_request, issue, etc
    payload JSONB NOT NULL,
    
    -- Processing status
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending, processing, processed, failed
    error_message TEXT,
    processed_at TIMESTAMP,
    
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    CONSTRAINT fk_webhooks_repository FOREIGN KEY (repository_id) REFERENCES repositories(id)
);

CREATE INDEX idx_webhooks_repository_id ON webhooks(repository_id);
CREATE INDEX idx_webhooks_event_type ON webhooks(event_type);
CREATE INDEX idx_webhooks_status ON webhooks(status);
CREATE INDEX idx_webhooks_created_at ON webhooks(created_at DESC);

-- ========================
-- PLACEHOLDER TABLES (to be implemented)
-- ========================

-- Infrastructure resources (for Infra Hub)
-- CREATE TABLE infrastructure_resources (...)

-- Architecture decisions / ADRs (for Architecture Hub)
-- CREATE TABLE architecture_decisions (...)

-- Code analyses (for Code Hub)
-- CREATE TABLE code_analyses (...)

-- Code embeddings (for semantic search)
-- CREATE TABLE code_embeddings (...)

-- Service dependencies (for Architecture/Infra Hub)
-- CREATE TABLE service_dependencies (...)

-- ========================
-- AUDIT TABLE (optional, for tracking changes)
-- ========================

-- CREATE TABLE audit_logs (
--     id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
--     table_name VARCHAR(255) NOT NULL,
--     record_id UUID NOT NULL,
--     operation VARCHAR(10) NOT NULL, -- INSERT, UPDATE, DELETE
--     old_data JSONB,
--     new_data JSONB,
--     user_id UUID REFERENCES users(id),
--     created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
-- );