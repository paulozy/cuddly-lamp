-- 024 — Drop the dependency-tracking leftovers and the now-unused pgvector
-- extension.
--
-- Dependency scanning was Claude-powered and its worker/handler were removed in
-- an earlier slim-down, but the tables and Go models survived as dead weight.
-- `repository_dependencies` was already legacy: migration 012 backfilled it into
-- `repository_relationships`, which is the canonical graph model.
--
-- pgvector only ever backed `code_embeddings`, dropped in migration 023.
--
-- THIS IS DESTRUCTIVE AND IRREVERSIBLE. Take a dump first if the package
-- inventory is worth keeping.
--
-- All steps are idempotent.

-- 1. Drop the dependency tables.
DROP TABLE IF EXISTS package_dependencies CASCADE;
DROP TABLE IF EXISTS repository_dependencies CASCADE;

-- 2. Drop pgvector. Guarded so the migration still succeeds on a database where
--    the extension was never installed, and CASCADE-free so it fails loudly if
--    some object we didn't account for still depends on it.
DROP EXTENSION IF EXISTS vector;
