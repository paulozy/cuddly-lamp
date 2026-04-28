-- Migration 006: Convert sensitive plaintext columns to bytea for encrypted storage
--
-- IMPORTANT: Run cmd/migrate-encrypt BEFORE applying this migration in production.
-- That tool reads existing plaintext values, encrypts them, and writes back the
-- encrypted bytes as hex strings into the TEXT columns. This migration then
-- decodes those hex strings into bytea. NULL and empty values become NULL bytea.

ALTER TABLE oauth_connections
    ALTER COLUMN access_token TYPE bytea
    USING CASE
        WHEN access_token IS NULL OR access_token = '' THEN NULL
        ELSE decode(access_token, 'hex')
    END;

ALTER TABLE webhook_configs
    ALTER COLUMN secret TYPE bytea
    USING CASE
        WHEN secret IS NULL OR secret = '' THEN NULL
        ELSE decode(secret, 'hex')
    END;
