-- 025 — Invite-only membership for existing organizations.
--
-- Until now `POST /api/v1/auth/register` accepted an `organization_slug` and, if an
-- organization with that slug existed, silently added the caller as `developer`.
-- Slugs are derived from organization names, so anyone who could guess one could
-- join. This table is the gate: joining an existing organization now requires a
-- valid invite issued by an admin.
--
-- Invites follow the same shape as `coverage_upload_tokens` (migration 018): the
-- plaintext token is shown exactly once on creation and only its SHA-256 hash is
-- stored, so a database read cannot recover a usable invite. Revocation and
-- acceptance flip timestamps instead of deleting rows, keeping an audit trail of who
-- let whom in.

CREATE TABLE IF NOT EXISTS organization_invites (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- The invite is bound to an address: accepting it requires registering with the
    -- same e-mail, so a leaked link cannot be redeemed by whoever finds it.
    email               VARCHAR(255) NOT NULL,
    role                VARCHAR(50)  NOT NULL DEFAULT 'developer'
                        CHECK (role IN ('viewer', 'developer', 'maintainer', 'admin')),

    token_hash          VARCHAR(64) NOT NULL UNIQUE,

    created_by_user_id  UUID REFERENCES users(id) ON DELETE SET NULL,
    accepted_at         TIMESTAMP,
    accepted_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    revoked_at          TIMESTAMP,
    expires_at          TIMESTAMP NOT NULL,
    created_at          TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);

-- At most one live invite per address per organization, so re-inviting someone
-- replaces rather than accumulates. Case-insensitive because e-mail domains are.
CREATE UNIQUE INDEX IF NOT EXISTS idx_organization_invites_pending_email
    ON organization_invites (organization_id, lower(email))
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- Listing pending invites for the organization settings screen.
CREATE INDEX IF NOT EXISTS idx_organization_invites_org
    ON organization_invites (organization_id, created_at DESC);
