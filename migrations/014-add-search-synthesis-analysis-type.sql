-- Allow `search_synthesis` as a valid analysis type so AI summaries of
-- semantic-search results can be persisted in code_analyses for token-budget
-- accounting via SumTokensUsedSince.
ALTER TABLE code_analyses
    DROP CONSTRAINT IF EXISTS code_analyses_type_check;

ALTER TABLE code_analyses
    ADD CONSTRAINT code_analyses_type_check
    CHECK (type IN ('code_review', 'metrics', 'dependency', 'security', 'architecture', 'search_synthesis'));
