-- 030 — Stop the imported-team index from colliding on locally created teams.
--
-- `idx_teams_org_external` (migration 026) exists to keep one imported team per
-- (organization, provider, external id). Its predicate said:
--
--     WHERE external_id IS NOT NULL AND deleted_at IS NULL
--
-- which reads as "only rows that carry an external id" — but that is not what
-- it selects. `models.Team.ExternalID` is a plain `string`, not a pointer, and
-- the model documents that provider/external_id "stay empty for locally managed
-- teams". So a local team is inserted with `''`, and `'' IS NOT NULL` is true.
--
-- Every locally created team therefore landed in the index under the same
-- ('', '') pair, and the *second* team any organization created failed with:
--
--     duplicate key value violates unique constraint "idx_teams_org_external"
--
-- surfaced by the API as a bare 500 "team operation failed". The bug has been
-- there since teams shipped; it only bites once an organization wants more than
-- one team.
--
-- The fix is to make the predicate mean what it says. Empty string is this
-- schema's representation of "no external id" — the model is explicit about
-- that — so the index has to exclude it alongside NULL.

DROP INDEX IF EXISTS idx_teams_org_external;

CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_org_external
    ON teams (organization_id, provider, external_id)
    WHERE external_id IS NOT NULL
      AND external_id <> ''
      AND deleted_at IS NULL;
