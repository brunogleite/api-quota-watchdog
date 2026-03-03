-- 001_init.sql
-- Initial schema for API Quota Watchdog.
-- Run once against a fresh database: psql $DATABASE_URL -f db/migrations/001_init.sql

-- providers holds every registered upstream API provider.
-- api_key_value is the server-side credential injected by the proxy;
-- it is never exposed to callers.
CREATE TABLE providers (
    id             BIGSERIAL    PRIMARY KEY,
    name           TEXT         NOT NULL UNIQUE,
    base_url       TEXT         NOT NULL,
    api_key_header TEXT         NOT NULL,
    api_key_value  TEXT         NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- services identifies internal callers that route requests through the watchdog.
-- service_id = NULL in usage_records means the caller is unidentified (no JWT claim yet).
CREATE TABLE services (
    id         BIGSERIAL   PRIMARY KEY,
    name       TEXT        NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- quotas defines the request limit and billing window for a provider.
-- One row per provider. window_start marks the beginning of the current window;
-- reset it to NOW() to start a new billing period.
-- threshold_crossed is the edge-trigger flag: set to TRUE when an alert fires,
-- reset to FALSE when the window resets, ensuring alerts fire only once per crossing.
CREATE TABLE quotas (
    id                BIGSERIAL   PRIMARY KEY,
    provider_id       BIGINT      NOT NULL UNIQUE REFERENCES providers(id),
    request_limit     BIGINT      NOT NULL CHECK (request_limit > 0),
    window_start      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    threshold_crossed BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- usage_records is the append-only log of every proxied request.
-- service_id is NULL when the caller has not been identified via JWT claims.
CREATE TABLE usage_records (
    id          BIGSERIAL   PRIMARY KEY,
    provider_id BIGINT      NOT NULL REFERENCES providers(id),
    service_id  BIGINT      REFERENCES services(id),
    method      TEXT        NOT NULL,
    status_code INT         NOT NULL,
    latency_ms  BIGINT      NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Covering index for the quota window query: filters by provider and time range.
CREATE INDEX idx_usage_records_provider_recorded
    ON usage_records (provider_id, recorded_at);
