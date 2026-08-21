-- 028 — GitLab as a first-class provider.
--
-- The catalog has always accepted gitlab.com URLs, but sync refused them: the
-- only client was GitHub's, and `owner/repo` is not unique across forges, so
-- running a GitLab path through it would have imported an unrelated project's
-- data. With a real GitLab client in place, what was missing is the credential
-- to call it with.
--
-- `gitlab_token` mirrors `github_token`: encrypted at rest through the same
-- GORM serializer, so it is a bytea holding AES-256-GCM ciphertext, never
-- readable plaintext. It is separate from `gitlab_client_secret`, which is the
-- OAuth app secret used to log users in — a different credential with a
-- different lifetime, and conflating them would mean a login secret silently
-- gaining API write access to every project.
--
-- `gitlab_base_url` is added now and read by nothing yet. Self-hosted GitLab is
-- out of scope for this release, but the column costs nothing today and saves a
-- migration when it lands.

ALTER TABLE organization_configs
    ADD COLUMN IF NOT EXISTS gitlab_token bytea,
    ADD COLUMN IF NOT EXISTS gitlab_base_url text;

COMMENT ON COLUMN organization_configs.gitlab_token IS
    'Encrypted GitLab access token used for project sync, merge request reads, webhook registration and documentation MRs.';
COMMENT ON COLUMN organization_configs.gitlab_base_url IS
    'Reserved for self-hosted GitLab. Unused: the client is pinned to https://gitlab.com/api/v4.';
