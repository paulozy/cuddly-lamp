-- 033 — derived relationships: provenance, dedup, sweep, and human tombstones
--
-- Derivation runs on every sync, so it needs to be able to insert an edge it
-- already inserted, and to retract an edge that disappeared, without ever
-- touching an edge a person declared. Three columns and two indexes buy all of
-- that; the design is Backstage's entity-provider `locationKey`, whose conflict
-- rule is verbatim: "If the existing entity has no location key, the new entity
-- wins. If the existing entity has a location key, the new entity only wins
-- when the location keys match."
--
-- `derivation_key` is that string: `<deriver>:<version>:<scope>`, e.g.
-- `libdep:v1:org/<uuid>`. NULL means a human declared the row. Every
-- destructive statement is scoped by `derivation_key = $1`, which makes
-- deleting a human row *structurally* impossible rather than a matter of
-- discipline — NULL never equals anything.
--
-- The version segment is what allows re-keying on purpose when a deriver's
-- logic changes: bump to `libdep:v2:...` and the first run of v2 leaves every
-- v1 row unclaimed, so the v1 sweep retires them. Changing behaviour silently
-- under the same key is the failure mode this avoids.
--
-- `derivation_fingerprint` is the identity of the *fact* behind the edge:
-- rule id + ecosystem + package name + evidence path. Notably absent from it is
-- the declared version, which lives in `metadata` instead — a version bump must
-- not sweep and recreate the edge, because that would reset the row's id and
-- break every deep link to it.
--
-- Migration 012 shipped with no unique constraint at all, and a test documents
-- multiplicity between the same two repositories as intentional:
-- `(a→b, http)` and `(a→b, async)` coexist by design. So the index below is
-- PARTIAL — `WHERE derivation_key IS NOT NULL` — and never constrains a human
-- row. That test must keep passing.
--
-- Deliberately not done: no `ON DELETE` behaviour change and no touching of
-- `source`'s existing values. `manifest` was already in the CHECK and never
-- used; it is what phase 1's library edges use. Only `config` is new.

-- ── derivation provenance on repository_relationships ────────────────────────

ALTER TABLE repository_relationships
    ADD COLUMN IF NOT EXISTS derivation_key TEXT;
ALTER TABLE repository_relationships
    ADD COLUMN IF NOT EXISTS derivation_fingerprint TEXT;
-- TIMESTAMP, not TIMESTAMPTZ: this table's other timestamps are TIMESTAMP and
-- mixing them would make the mark-and-sweep comparison depend on the session
-- time zone.
ALTER TABLE repository_relationships
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMP;

COMMENT ON COLUMN repository_relationships.derivation_key IS
    'NULL means a human declared this row; migration 033 — every sweep is scoped by this value';

CREATE UNIQUE INDEX IF NOT EXISTS idx_repo_rel_derived_identity
    ON repository_relationships
       (organization_id, source_repository_id, target_repository_id, kind,
        derivation_key, derivation_fingerprint)
    WHERE derivation_key IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_repo_rel_derivation_sweep
    ON repository_relationships (derivation_key, last_seen_at)
    WHERE derivation_key IS NOT NULL AND deleted_at IS NULL;

-- ── source gains 'config' ────────────────────────────────────────────────────
--
-- Postgres has no ADD CONSTRAINT IF NOT EXISTS, so the guarded block is the
-- house pattern (see 031:41-50 and 022:29-48). Precedent for altering an
-- existing enum CHECK: 014-add-search-synthesis-analysis-type.sql.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint
                WHERE conname = 'repository_relationships_source_check') THEN
        ALTER TABLE repository_relationships
            DROP CONSTRAINT repository_relationships_source_check;
    END IF;
    ALTER TABLE repository_relationships
        ADD CONSTRAINT repository_relationships_source_check
        CHECK (source IN ('manual','analysis','manifest','import','webhook',
                          'legacy_dependency','config'));
END $$;

-- ── kind gains 'provides' and 'uses' ─────────────────────────────────────────
--
-- Phase 2 needs repo→api and phase 3 needs repo→resource. Reusing `other`
-- would make the graph unable to say what an edge means, which is the whole
-- point of a typed graph.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint
                WHERE conname = 'repository_relationships_kind_check') THEN
        ALTER TABLE repository_relationships
            DROP CONSTRAINT repository_relationships_kind_check;
    END IF;
    ALTER TABLE repository_relationships
        ADD CONSTRAINT repository_relationships_kind_check
        CHECK (kind IN ('http','async','library','data','infra','manual','other',
                        'provides','uses'));
END $$;

-- ── human rejection of a derived edge ────────────────────────────────────────
--
-- A soft delete does not survive re-derivation: the next run recomputes the
-- same fingerprint, finds the soft-deleted twin, and revives it. "I dismissed
-- that edge" therefore needs a table of its own, consulted by the deriver
-- before it writes anything.

CREATE TABLE IF NOT EXISTS derivation_suppressions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    derivation_key         TEXT NOT NULL,
    derivation_fingerprint TEXT NOT NULL,
    reason TEXT,
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_derivation_suppressions_identity
    ON derivation_suppressions (organization_id, derivation_key, derivation_fingerprint);
