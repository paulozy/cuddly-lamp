-- 031 — Name what already exists: a documentation row that no model wrote.
--
-- `doc_generations` is named for an AI run, and most of its columns describe
-- one: `status`, `progress_stage`, `tokens_used`, `error_message`,
-- `pull_request_url`. But rows that are not AI runs have been landing in this
-- table since org docs shipped — `DocsHandler.UpdateDocContent` inserts one
-- every time somebody edits a document by hand, with `status = 'completed'`,
-- `tokens_used = 0`, no progress stage and no pull request.
--
-- So this column does not introduce a new kind of row. It gives a name to the
-- kind that was already there, which is what makes hand-written documentation
-- offerable as a first-class action instead of something you reach by
-- generating a document with Claude and overwriting its content.
--
-- Why a column rather than inference: "manual" could be guessed from the
-- absence of things — `tokens_used = 0`, no PR, no progress stage. That is
-- implicit provenance: not indexable, not queryable, and wrong on the first
-- edge case. A *failed* AI generation also has `tokens_used = 0`, and org-scope
-- generations legitimately have no pull request. Guessing would misfile both.
--
-- VARCHAR + CHECK rather than a Postgres enum, matching migrations 011 and 019
-- — adding a value to an enum is a schema migration, and this vocabulary may
-- well grow (an imported doc, a doc synced back from the repository).

ALTER TABLE doc_generations
    ADD COLUMN IF NOT EXISTS source VARCHAR(8) NOT NULL DEFAULT 'ai';

-- Backfill is uniform: every existing row is marked 'ai'.
--
-- This is a deliberate loss. The hand-edited rows described above *could* be
-- identified heuristically:
--
--     tokens_used = 0 AND status = 'completed' AND pull_request_url IS NULL
--
-- but that predicate also matches a generation that failed before spending a
-- token, and it cannot distinguish an edit from a generation that produced
-- nothing. Historical provenance is cosmetic — nothing reads it to make a
-- decision — so a wrong answer is worse than a uniform one. Rows created from
-- here on are labelled correctly at insert time.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'doc_generations_source_check'
    ) THEN
        ALTER TABLE doc_generations
            ADD CONSTRAINT doc_generations_source_check
            CHECK (source IN ('ai', 'manual'));
    END IF;
END $$;

COMMENT ON COLUMN doc_generations.source IS
    'Who produced this row: ''ai'' for a Claude generation, ''manual'' for a document written or edited by a person. Backfilled uniformly as ''ai'' in migration 031 — see that file for why the historical split was not reconstructed.';
