-- Allow each organization to choose the language used for AI-generated prose
-- (analysis summaries, generated documentation, code template summaries,
-- search synthesis, PR review bodies). Stored as a BCP 47 tag (e.g. "en",
-- "pt-BR"). Validation happens at the application layer; default keeps
-- existing behaviour (English) for all rows.
ALTER TABLE organization_configs
    ADD COLUMN IF NOT EXISTS output_language VARCHAR(20) NOT NULL DEFAULT 'en';
