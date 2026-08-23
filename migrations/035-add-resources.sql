-- 035 — the `resources` entity: runtime infrastructure, and an honest limit
--
-- The limit first, because it shapes every column below: **you cannot, from
-- static files, put two repositories on the same production Postgres.** The
-- reason is structural, not effort. Terraform (or a cloud console) *declares* the
-- resource in one repository; consumers *reference* it by a hostname that arrives
-- at runtime in a secret. Neither side has the other in any committed file.
--
-- Everything that delivers trustworthy resource topology derives it from runtime
-- telemetry — Istio/Kiali, Weave Scope, Datadog, Dash0, OpenTelemetry's
-- `spanmetrics` connector. The consensus, in one line: the only accurate
-- dependency map is one generated from actual production traffic.
--
-- So v1 delivers a reliable *inventory* ("this repository uses a Postgres, and
-- here is the proof") and unifies two repositories only when there is evidence of
-- identity: the same (engine, host, port, namespace) in both. Where there is no
-- such evidence the resource is scoped to its repository and the UI says so. Two
-- repositories each with `postgres:16` in a compose file are TWO nodes, because
-- they are not the same Postgres. That is the rule that keeps the graph from
-- becoming a pretty lie: a single "Postgres" node with forty edges arriving,
-- corresponding to no database that exists.
--
-- That is not a defeat. Inventory alone answers "which services use Kafka?",
-- which today has no answer at all.
--
-- The locator mirrors OpenTelemetry's database semantic conventions rather than
-- inventing an identity: engine is `db.system.name`, host is `server.address`,
-- port is `server.port`, namespace is `db.namespace` (the logical database inside
-- the instance). The practical payoff is forward-looking — when observability
-- integration arrives, a resource derived statically and one observed in a trace
-- unify with no translation layer.
--
-- Deliberately not done: Terraform. `terraform-config-inspect` is the right
-- shallow parser, but it needs a directory on disk (which would force the clone
-- this whole domain was designed to avoid), it is MPL-2.0, and it deliberately
-- does not evaluate `var.` or `local.` — which is precisely where a resource's
-- identity lives. It would buy inventory, not the join, at the price of a clone.
--
-- Credentials are never stored. A DSN carries `user:pass`; the parser discards it
-- before anything is persisted, and it appears in no column, no metadata and no
-- evidence log.

CREATE TABLE IF NOT EXISTS resources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- The OTel-shaped locator.
    engine    VARCHAR(50) NOT NULL,   -- db.system.name
    host      VARCHAR(255),           -- server.address
    port      INTEGER,                -- server.port
    namespace VARCHAR(255),           -- db.namespace, the logical database

    -- NULL means shared: the host is known, so two repositories pointing at it
    -- are pointing at the same thing. Non-NULL means the only evidence was a
    -- compose file or a Helm subchart, which describes *an* engine and not
    -- *which instance* — so the row belongs to one repository and must never be
    -- unified with another's.
    scoped_repository_id UUID REFERENCES repositories(id) ON DELETE CASCADE,

    display_name VARCHAR(255),

    derivation_key         TEXT,
    derivation_fingerprint TEXT,
    last_seen_at TIMESTAMP,

    metadata JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    deleted_at TIMESTAMP
);

-- Shared resource: the locator is the identity, so two repositories naming the
-- same host converge on one row. This index is the unification.
--
-- Postgres treats NULLs as distinct in a unique index, so `port` or `namespace`
-- being NULL would defeat it. COALESCE to sentinels that cannot occur in real
-- values keeps the identity total.
CREATE UNIQUE INDEX IF NOT EXISTS idx_resources_shared_identity
    ON resources (organization_id, engine, host, COALESCE(port, -1), COALESCE(namespace, ''))
    WHERE scoped_repository_id IS NULL AND deleted_at IS NULL;

-- Scoped resource: the identity includes the repository, which is what makes
-- "a local postgres" in two repositories two nodes. That is the desired answer.
CREATE UNIQUE INDEX IF NOT EXISTS idx_resources_scoped_identity
    ON resources (organization_id, scoped_repository_id, engine, COALESCE(display_name, ''))
    WHERE scoped_repository_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_resources_org ON resources (organization_id);

DROP TRIGGER IF EXISTS resources_updated_at_trigger ON resources;
CREATE TRIGGER resources_updated_at_trigger
    BEFORE UPDATE ON resources
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

COMMENT ON COLUMN resources.scoped_repository_id IS
    'NULL = shared (host known); non-NULL = evidence was compose/subchart only, never unify across repositories; migration 035';

-- ── the join ─────────────────────────────────────────────────────────────────
--
-- Two tables rather than one because a shared resource has N consumers, and it is
-- the join that answers "do these three services depend on the same Postgres?".

CREATE TABLE IF NOT EXISTS repository_resources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    repository_id   UUID NOT NULL REFERENCES repositories(id)  ON DELETE CASCADE,
    resource_id     UUID NOT NULL REFERENCES resources(id)     ON DELETE CASCADE,

    confidence NUMERIC(5,4) NOT NULL DEFAULT 1.0
        CHECK (confidence >= 0 AND confidence <= 1),

    derivation_key         TEXT,
    derivation_fingerprint TEXT,
    last_seen_at TIMESTAMP,
    metadata JSONB NOT NULL DEFAULT '{}',   -- rule_id, evidence paths

    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_repository_resources_derived_identity
    ON repository_resources (repository_id, resource_id, derivation_key, derivation_fingerprint)
    WHERE derivation_key IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_repository_resources_resource
    ON repository_resources (resource_id) WHERE deleted_at IS NULL;

DROP TRIGGER IF EXISTS repository_resources_updated_at_trigger ON repository_resources;
CREATE TRIGGER repository_resources_updated_at_trigger
    BEFORE UPDATE ON repository_resources
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
