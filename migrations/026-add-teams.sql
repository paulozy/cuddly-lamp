-- 026 — Team ownership for repositories.
--
-- An IDP's central question is "who do I call when this breaks". Until now a
-- repository carried `owner_user_id`, which `repository_service.go` set to the
-- creating user — the same value as `created_by_user_id`. It named a person, not
-- an accountable group, and nothing in the UI ever read it.
--
-- Teams replace it. Ownership is a single nullable FK to a team, which is what
-- Backstage, Cortex and OpsLevel all converge on: "one accountable owner" has an
-- answer, "a set of owners" does not. Multi-owner and team hierarchy are both
-- additive later if they earn their keep.
--
-- `owner_team_id` is deliberately NOT backfilled. NULL means unowned, which is the
-- truth for every existing row and the exact input the first scorecard check
-- ("does this service have an owner?") needs. Backfilling to a synthetic default
-- team would fabricate ownership and make that check useless on day one.

-- ── teams ────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS teams (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    name        VARCHAR(255) NOT NULL,
    slug        VARCHAR(120) NOT NULL,
    description TEXT,

    -- Reserved for importing teams from the git provider later. GitHub teams must
    -- be keyed on the numeric id, never the slug: renaming a team regenerates its
    -- slug, and a slug-keyed sync would silently create duplicates. Kept NULL
    -- while teams are managed locally.
    provider    VARCHAR(50),
    external_id VARCHAR(255),

    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_org_slug
    ON teams (organization_id, slug)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_org_external
    ON teams (organization_id, provider, external_id)
    WHERE external_id IS NOT NULL AND deleted_at IS NULL;

-- ── team members ─────────────────────────────────────────────────────────────
--
-- A first-class table rather than a GORM `many2many` tag, matching how
-- `organization_members` is already modelled. Association mode would clobber the
-- extra columns on write.

CREATE TABLE IF NOT EXISTS team_members (
    team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Team-local role, distinct from the organization role. A lead may curate
    -- membership; it grants nothing outside the team.
    role VARCHAR(50) NOT NULL DEFAULT 'member'
         CHECK (role IN ('member', 'lead')),

    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),

    PRIMARY KEY (team_id, user_id)
);

-- The PK covers team → members. This covers the hot direction: "which teams am I
-- on", which drives both the catalog filter and the write-permission check.
CREATE INDEX IF NOT EXISTS idx_team_members_user
    ON team_members (user_id, team_id);

-- ── repository ownership ─────────────────────────────────────────────────────

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS owner_team_id UUID REFERENCES teams(id) ON DELETE SET NULL;

-- "repositories owned by my teams", the catalog's default filter.
CREATE INDEX IF NOT EXISTS idx_repositories_owner_team
    ON repositories (organization_id, owner_team_id, created_at DESC)
    WHERE deleted_at IS NULL;

-- "which services have no owner" — the first scorecard check.
CREATE INDEX IF NOT EXISTS idx_repositories_unowned
    ON repositories (organization_id)
    WHERE owner_team_id IS NULL AND deleted_at IS NULL;

-- ── retire the previous attempts ─────────────────────────────────────────────

-- owner_user_id duplicated created_by_user_id and was never read by the UI.
DROP INDEX IF EXISTS idx_repositories_owner_user_id;
ALTER TABLE repositories DROP COLUMN IF EXISTS owner_user_id;

-- user_repositories was a pre-multi-tenancy per-repo ACL. It has a GORM relation
-- declared on both User and Repository and zero readers or writers anywhere in
-- the service layer. Leaving it beside a real ownership model guarantees someone
-- eventually wires up the wrong one.
DROP TABLE IF EXISTS user_repositories CASCADE;
