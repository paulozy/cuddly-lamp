-- 034 — the `apis` entity: what a repository exposes
--
-- Without it the graph can say that A depends on B and nothing about *what* B
-- offers. In Backstage's model `API` is the kind representing "the boundaries and
-- contracts between components", and it is additive here for the same reason: an
-- API inherits its repository's owner, so this table touches neither ownership,
-- nor the scorecard, nor docs.
--
-- The identity is `(repository_id, spec_path)` — the location, which is literally
-- Backstage's `locationKey` applied to a file. Three consequences, all wanted:
--
--   * `info.title` is display. Renaming the title does not create a new API.
--   * `info.version` is a mutable attribute and is deliberately NOT in the key.
--     If it were, every version bump would create a new API and sweep the old
--     one, manufacturing a history that never happened.
--   * Moving or renaming the file is a new API and a sweep of the old one. That
--     is correct: the catalog says where the contract lives *now*.
--
-- `operation_count` is nullable on purpose. A spec whose `paths` are pulled in by
-- `$ref` from another file yields an incomplete count, and the UI shows "—"
-- rather than a confident zero. We are extracting identity, not the contract:
-- counting operations properly needs a `$ref` resolver, which is a whole library
-- (kin-openapi validates on load, libopenapi keeps a full AST) and neither is
-- worth carrying to fill in one integer.
--
-- Deliberately not done: no separate `api_versions` table, no spec content
-- stored. The file lives in the repository and the provider serves it; copying it
-- here would create a second, stale truth.

CREATE TABLE IF NOT EXISTS apis (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    repository_id   UUID NOT NULL REFERENCES repositories(id)  ON DELETE CASCADE,

    -- The location IS the identity.
    spec_path VARCHAR(1024) NOT NULL,

    -- Closed set, so it gets a CHECK (the reasoning in migration 029 §17-20).
    kind VARCHAR(30) NOT NULL
        CHECK (kind IN ('openapi', 'asyncapi', 'graphql', 'grpc')),

    -- Display, not identity.
    title   VARCHAR(500),
    version VARCHAR(100),
    operation_count INTEGER,

    -- Same reconciliation primitives as migration 033. Sweeps here are scoped by
    -- repository, not organization: API discovery is a purely local fact — what
    -- exists in this repository's tree — so it needs no cross-repository index
    -- and must not wait for every repository to sync.
    derivation_key         TEXT,
    derivation_fingerprint TEXT,
    last_seen_at TIMESTAMP,

    metadata JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_apis_identity
    ON apis (repository_id, spec_path)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_apis_org ON apis (organization_id);
CREATE INDEX IF NOT EXISTS idx_apis_derivation ON apis (derivation_key, last_seen_at)
    WHERE derivation_key IS NOT NULL;

DROP TRIGGER IF EXISTS apis_updated_at_trigger ON apis;
CREATE TRIGGER apis_updated_at_trigger
    BEFORE UPDATE ON apis
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

COMMENT ON COLUMN apis.spec_path IS
    'the location is the identity; migration 034 — moving the file is a new API, not a renamed one';
COMMENT ON COLUMN apis.operation_count IS
    'NULL when $ref made the count unreliable; migration 034 — the UI shows a dash, never a confident zero';
