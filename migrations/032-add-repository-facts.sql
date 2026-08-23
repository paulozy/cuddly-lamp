-- 032 — extracted facts, one row per (repository, fact kind)
--
-- Architecture derivation runs in two passes: extraction, which is per
-- repository and does network I/O, and reconciliation, which is per
-- organization and is a pure function. This table is the seam between them.
-- Without it every re-derivation would have to re-read every repository,
-- because an internal edge is inherently org-wide: knowing that A depends on B
-- needs the index of names published by *every* repository in the org.
--
-- `payload` is JSONB and not columns on purpose. The shape of a fact changes
-- with every new extractor (a new ecosystem, a new sniff), and columnising that
-- would mean one migration per rule. `extractor_version` is what allows a
-- deliberate reprocess when the extraction logic changes; the reconciler — Go
-- code, testable against literals — is what gives the payload meaning.
--
-- `complete` is the most important column here, and the reason this table
-- exists at all rather than the reconciler reading trees directly. A truncated
-- listing, a 429 or a 5xx are indistinguishable from "the dependency was
-- removed", and a sweep that cannot tell them apart deletes good edges on a
-- bad afternoon. So the sweep is gated on it, and it is `false` by default:
-- an extraction that dies halfway leaves a row that can never authorise a
-- delete.
--
-- Deliberately not done: no per-file rows. The unit of work is (repository,
-- fact kind), because that is the unit the tree SHA guards — same tree, same
-- facts, skip the network entirely.

CREATE TABLE IF NOT EXISTS repository_facts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    repository_id   UUID NOT NULL REFERENCES repositories(id)  ON DELETE CASCADE,

    -- Closed set, so it gets a CHECK — matching the reasoning in migration 029:
    -- status has a CHECK because it is closed, onboarding step kind does not
    -- because it grows with the code. Rule ids live in `payload` for exactly
    -- that reason and are deliberately unconstrained.
    fact_kind VARCHAR(50) NOT NULL
        CHECK (fact_kind IN ('packages', 'apis', 'resources', 'hosts')),

    payload JSONB NOT NULL DEFAULT '{}',

    -- tree_sha is the guard against unnecessary work: same tree, same facts.
    tree_sha VARCHAR(64),

    complete BOOLEAN NOT NULL DEFAULT false,

    extractor_version INTEGER NOT NULL DEFAULT 1,
    extracted_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),

    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_repository_facts_identity
    ON repository_facts (repository_id, fact_kind);
CREATE INDEX IF NOT EXISTS idx_repository_facts_org
    ON repository_facts (organization_id, fact_kind);

DROP TRIGGER IF EXISTS repository_facts_updated_at_trigger ON repository_facts;
CREATE TRIGGER repository_facts_updated_at_trigger
    BEFORE UPDATE ON repository_facts
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

COMMENT ON COLUMN repository_facts.complete IS
    'false when the tree was truncated or a read failed; migration 032 — the reconciler never sweeps from an incomplete fact';
