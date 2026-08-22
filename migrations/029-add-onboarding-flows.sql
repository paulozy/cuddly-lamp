-- 029 — Configurable onboarding for new members.
--
-- The platform already knows what a new developer needs to learn: which
-- repositories exist, what each one is built with, which team answers for it,
-- how they relate, and the documentation generated for each. What was missing
-- was a path through it. This is that path: an admin composes one or more
-- flows, an invite carries the flow the new person should walk, and the
-- application guides them from "welcome" to a first real task.
--
-- The central modelling decision: a step REFERENCES a live entity rather than
-- copying it. A step about a team stores the team id, not a paragraph naming
-- its members — otherwise the onboarding rots on the first rename, and an
-- outdated onboarding is worse than none because it teaches the wrong thing
-- with authority. Only genuinely editorial content (welcome, culture, "how we
-- work") is stored as markdown on the step.
--
-- `kind` deliberately carries no CHECK constraint: the set of step kinds grows
-- with the code that renders them, and a constraint would mean a migration per
-- kind. Statuses do get CHECKs — those sets are closed and stable, which is
-- the same line migration 022 drew for repository status.

-- ── flows ────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS onboarding_flows (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    name        VARCHAR(255) NOT NULL,
    slug        VARCHAR(120) NOT NULL,
    description TEXT,

    -- The flow an invite falls back to when it names none. At most one per
    -- organization, enforced below rather than by convention.
    is_default BOOLEAN NOT NULL DEFAULT FALSE,

    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_onboarding_flows_org_slug
    ON onboarding_flows (organization_id, slug)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_onboarding_flows_default
    ON onboarding_flows (organization_id)
    WHERE is_default AND deleted_at IS NULL;

-- ── steps ────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS onboarding_steps (
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flow_id UUID NOT NULL REFERENCES onboarding_flows(id) ON DELETE CASCADE,

    -- Ordering is rewritten from array order whenever the step list is saved,
    -- so no uniqueness is enforced here: a transient duplicate mid-rewrite is
    -- not worth a deferrable constraint.
    position INTEGER NOT NULL,

    kind  VARCHAR(50)  NOT NULL,
    title VARCHAR(255) NOT NULL,

    -- Markdown, for the editorial kinds only.
    body TEXT,

    -- Kind-specific references and options: repository_id, team_id,
    -- doc_generation_id, term_ids, people, items, url, check. Shape is
    -- validated in Go against the kind, because the union differs per kind and
    -- a column per field would be mostly NULL.
    config JSONB NOT NULL DEFAULT '{}'::jsonb,

    is_required BOOLEAN NOT NULL DEFAULT TRUE,

    -- "≈10 min", so someone can plan their week. NULL means unstated, which is
    -- different from zero.
    estimated_minutes INTEGER,

    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

CREATE INDEX IF NOT EXISTS idx_onboarding_steps_flow
    ON onboarding_steps (flow_id, position);

-- ── assignments ──────────────────────────────────────────────────────────────
--
-- A table rather than a flag on the membership, because there are N flows, a
-- person can be re-assigned (changing teams is a second onboarding), and the
-- progress rows need somewhere to hang.

CREATE TABLE IF NOT EXISTS onboarding_assignments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    flow_id         UUID NOT NULL REFERENCES onboarding_flows(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    assigned_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    -- The invite that produced this assignment, when it came from one. Kept for
    -- the audit trail: "who let this person in, and with which onboarding".
    invite_id UUID REFERENCES organization_invites(id) ON DELETE SET NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'pending'
           CHECK (status IN ('pending', 'in_progress', 'completed', 'abandoned')),

    started_at   TIMESTAMP,
    completed_at TIMESTAMP,

    -- "What was missing?", asked at the end. The newcomer is the only person
    -- who knows, and two weeks later they will have forgotten they didn't know.
    feedback    TEXT,
    feedback_at TIMESTAMP,

    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

-- One live assignment per person per flow. Abandoned rows are excluded so the
-- same flow can be assigned again later without deleting the history.
CREATE UNIQUE INDEX IF NOT EXISTS idx_onboarding_assignments_live
    ON onboarding_assignments (flow_id, user_id)
    WHERE status <> 'abandoned';

-- The admin dashboard reads by organization and status.
CREATE INDEX IF NOT EXISTS idx_onboarding_assignments_org
    ON onboarding_assignments (organization_id, status);

-- Resolving "does the caller have an onboarding in progress?" on every page
-- load of the home banner.
CREATE INDEX IF NOT EXISTS idx_onboarding_assignments_user
    ON onboarding_assignments (user_id, status);

-- ── progress ─────────────────────────────────────────────────────────────────
--
-- Progress is per step, not an index into the list. That is what makes live
-- edits work: a removed step disappears, a new step arrives pending, and what
-- was already read stays read. Only outcomes are stored — the absence of a row
-- means "pending", the same way `repositories.metadata.has_ci` uses NULL for
-- "not verified" rather than inventing a "no".

CREATE TABLE IF NOT EXISTS onboarding_step_progress (
    assignment_id UUID NOT NULL REFERENCES onboarding_assignments(id) ON DELETE CASCADE,
    step_id       UUID NOT NULL REFERENCES onboarding_steps(id) ON DELETE CASCADE,

    status VARCHAR(20) NOT NULL CHECK (status IN ('done', 'skipped')),
    -- Free text the person can leave on a step, e.g. why they skipped it.
    note TEXT,

    completed_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    created_at   TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at   TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),

    PRIMARY KEY (assignment_id, step_id)
);

-- ── glossary ─────────────────────────────────────────────────────────────────
--
-- Every company drowns newcomers in internal acronyms, and no wiki page stays
-- current. Terms live at organization scope, not inside a flow, because they
-- are useful outside the onboarding too — and because two flows referencing
-- the same term should not need two copies of it.

CREATE TABLE IF NOT EXISTS organization_glossary_terms (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    term       VARCHAR(120) NOT NULL,
    definition TEXT NOT NULL,

    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    updated_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC'),
    deleted_at TIMESTAMP
);

-- Case-insensitive, because "SLO" and "slo" are the same acronym.
CREATE UNIQUE INDEX IF NOT EXISTS idx_glossary_org_term
    ON organization_glossary_terms (organization_id, lower(term))
    WHERE deleted_at IS NULL;

-- ── wiring into what already exists ──────────────────────────────────────────

-- The invite carries the flow the invited person should walk. NULL means "use
-- the organization's default", and an organization with no default simply
-- produces no assignment — a normal state, not an error.
ALTER TABLE organization_invites
    ADD COLUMN IF NOT EXISTS onboarding_flow_id UUID REFERENCES onboarding_flows(id) ON DELETE SET NULL;

-- Verified steps need to recognise the person on the provider. Pull requests
-- and merge requests identify their author by username, while this table only
-- stored the numeric provider id — the OAuth profile call already reads the
-- username (`internal/oauth/github.go`, `gitlab.go`) and threw it away.
-- Existing rows stay NULL and are backfilled on the member's next login, so a
-- verified step reports "we could not confirm yet" rather than failing.
ALTER TABLE oauth_connections
    ADD COLUMN IF NOT EXISTS provider_username VARCHAR(255);

COMMENT ON COLUMN oauth_connections.provider_username IS
    'Provider login (GitHub `login`, GitLab `username`), used to match change request authorship in verified onboarding steps. NULL until the next login for connections created before migration 029.';
