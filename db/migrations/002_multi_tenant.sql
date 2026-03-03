-- 002_multi_tenant.sql
-- Multi-tenant schema: introduce users table, re-key providers to users,
-- remove server-side api_key_value, add mock_enabled toggle.
-- Run after 001_init.sql: psql $DATABASE_URL -f db/migrations/002_multi_tenant.sql

-- Add users table. Passwords are stored as bcrypt hashes only.
CREATE TABLE users (
    id            BIGSERIAL    PRIMARY KEY,
    email         TEXT         NOT NULL UNIQUE,
    password_hash TEXT         NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Drop FK constraints that reference providers before we recreate the table.
ALTER TABLE usage_records DROP CONSTRAINT usage_records_provider_id_fkey;
ALTER TABLE quotas        DROP CONSTRAINT quotas_provider_id_fkey;

DROP TABLE providers;

-- providers is now scoped to a user. The api_key_value column is removed:
-- the client supplies credentials in the forwarded request headers.
-- mock_enabled allows bypassing the real upstream for testing.
-- (user_id, name) must be unique so a user cannot register the same provider twice.
CREATE TABLE providers (
    id             BIGSERIAL    PRIMARY KEY,
    user_id        BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT         NOT NULL,
    base_url       TEXT         NOT NULL,
    api_key_header TEXT         NOT NULL,
    mock_enabled   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, name)
);

-- Re-attach the FK constraints to the new providers table.
ALTER TABLE usage_records
    ADD CONSTRAINT usage_records_provider_id_fkey
    FOREIGN KEY (provider_id) REFERENCES providers(id);

ALTER TABLE quotas
    ADD CONSTRAINT quotas_provider_id_fkey
    FOREIGN KEY (provider_id) REFERENCES providers(id);
