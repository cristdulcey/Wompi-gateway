-- 001_init.sql
-- Multi-Company Wompi Webhook Gateway Schema

CREATE TABLE IF NOT EXISTS companies (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS wompi_accounts (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    environment TEXT NOT NULL DEFAULT 'prod',
    events_secret TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS route_rules (
    id UUID PRIMARY KEY,
    wompi_account_id UUID NOT NULL REFERENCES wompi_accounts(id) ON DELETE CASCADE,
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    reference_prefix TEXT NOT NULL DEFAULT '',
    target_url TEXT NOT NULL,
    custom_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    max_retries INT NOT NULL DEFAULT 5,
    timeout_seconds INT NOT NULL DEFAULT 15,
    priority INT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_route_rules_account_active
    ON route_rules (wompi_account_id, active);

CREATE TABLE IF NOT EXISTS webhook_events (
    id UUID PRIMARY KEY,
    wompi_account_id UUID NOT NULL REFERENCES wompi_accounts(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    environment TEXT NOT NULL DEFAULT '',
    transaction_id TEXT NOT NULL DEFAULT '',
    reference TEXT NOT NULL DEFAULT '',
    transaction_status TEXT NOT NULL DEFAULT '',
    amount_in_cents BIGINT NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT '',
    checksum TEXT NOT NULL,
    signature_valid BOOLEAN NOT NULL DEFAULT TRUE,
    event_timestamp BIGINT NOT NULL DEFAULT 0,
    raw_payload TEXT NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending',
    matched_company_id UUID REFERENCES companies(id) ON DELETE SET NULL,
    matched_route_id UUID REFERENCES route_rules(id) ON DELETE SET NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_wompi_event_idempotency
        UNIQUE (wompi_account_id, transaction_id, event_type, transaction_status, checksum)
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_received_at
    ON webhook_events (received_at DESC);

CREATE INDEX IF NOT EXISTS idx_webhook_events_reference
    ON webhook_events (reference);

CREATE INDEX IF NOT EXISTS idx_webhook_events_status
    ON webhook_events (status);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY,
    webhook_event_id UUID NOT NULL REFERENCES webhook_events(id) ON DELETE CASCADE,
    route_rule_id UUID NOT NULL REFERENCES route_rules(id) ON DELETE CASCADE,
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    target_url TEXT NOT NULL,
    custom_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 5,
    timeout_seconds INT NOT NULL DEFAULT 15,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_status_code INT,
    last_error TEXT NOT NULL DEFAULT '',
    last_response_body TEXT NOT NULL DEFAULT '',
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_due
    ON webhook_deliveries (status, next_retry_at)
    WHERE status IN ('pending', 'retrying');

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_event
    ON webhook_deliveries (webhook_event_id);

CREATE TABLE IF NOT EXISTS delivery_attempts (
    id UUID PRIMARY KEY,
    delivery_id UUID NOT NULL REFERENCES webhook_deliveries(id) ON DELETE CASCADE,
    webhook_event_id UUID NOT NULL REFERENCES webhook_events(id) ON DELETE CASCADE,
    attempt_number INT NOT NULL,
    target_url TEXT NOT NULL,
    status_code INT,
    response_body TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    success BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_delivery_attempts_event
    ON delivery_attempts (webhook_event_id, created_at ASC);
