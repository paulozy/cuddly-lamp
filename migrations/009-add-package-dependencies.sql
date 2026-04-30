CREATE TABLE IF NOT EXISTS package_dependencies (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id        UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name                 VARCHAR(500) NOT NULL,
    current_version      VARCHAR(255) NOT NULL DEFAULT '',
    latest_version       VARCHAR(255) NOT NULL DEFAULT '',
    ecosystem            VARCHAR(50)  NOT NULL,
    manifest_file        VARCHAR(500) NOT NULL DEFAULT '',
    is_direct_dependency BOOLEAN NOT NULL DEFAULT true,
    is_vulnerable        BOOLEAN NOT NULL DEFAULT false,
    vulnerability_cves   TEXT[] NOT NULL DEFAULT '{}',
    update_available     BOOLEAN NOT NULL DEFAULT false,
    last_scanned_at      TIMESTAMP,
    created_at           TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at           TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    UNIQUE (repository_id, name, ecosystem)
);

CREATE INDEX IF NOT EXISTS idx_package_deps_repo_id ON package_dependencies(repository_id);
CREATE INDEX IF NOT EXISTS idx_package_deps_vulnerable ON package_dependencies(is_vulnerable) WHERE is_vulnerable = true;
