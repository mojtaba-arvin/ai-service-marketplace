CREATE TABLE IF NOT EXISTS payment_operations (
    id text PRIMARY KEY,
    provider_code text NOT NULL CHECK (provider_code ~ '^[a-z][a-z0-9_-]{1,40}$'),
    operation_type text NOT NULL CHECK (operation_type IN ('authorize','capture','refund')),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','succeeded','failed','unknown')),
    external_reference text,
    failure_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(provider_code,idempotency_key),
    CHECK (status <> 'succeeded' OR external_reference IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS payment_operation_events (
    id text PRIMARY KEY,
    operation_id text NOT NULL REFERENCES payment_operations(id) ON DELETE RESTRICT,
    event_type text NOT NULL,
    from_status text,
    to_status text NOT NULL,
    details text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS payment_webhook_receipts (
    id text PRIMARY KEY,
    provider_code text NOT NULL,
    event_id text NOT NULL,
    payload_hash bytea NOT NULL CHECK (octet_length(payload_hash)=32),
    received_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(provider_code,event_id)
);

CREATE INDEX IF NOT EXISTS payment_operations_status_idx
    ON payment_operations(provider_code,status,created_at);
CREATE INDEX IF NOT EXISTS payment_operation_events_operation_idx
    ON payment_operation_events(operation_id,created_at);

CREATE OR REPLACE FUNCTION reject_payment_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'payment audit events are append-only';
END $$;
DROP TRIGGER IF EXISTS payment_operation_events_immutable ON payment_operation_events;
CREATE TRIGGER payment_operation_events_immutable
BEFORE UPDATE OR DELETE ON payment_operation_events
FOR EACH ROW EXECUTE FUNCTION reject_payment_event_mutation();

CREATE OR REPLACE FUNCTION enforce_payment_operation_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.provider_code IS DISTINCT FROM OLD.provider_code
       OR NEW.operation_type IS DISTINCT FROM OLD.operation_type
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.amount_minor IS DISTINCT FROM OLD.amount_minor
       OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'payment operation request fields are immutable';
    END IF;
    IF OLD.status='pending' AND NEW.status IN ('pending','succeeded','failed','unknown') THEN
        RETURN NEW;
    END IF;
    IF OLD.status='unknown' AND NEW.status IN ('succeeded','failed') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'invalid payment operation status transition';
END $$;
DROP TRIGGER IF EXISTS payment_operation_state_guard ON payment_operations;
CREATE TRIGGER payment_operation_state_guard
BEFORE UPDATE ON payment_operations
FOR EACH ROW EXECUTE FUNCTION enforce_payment_operation_transition();

CREATE OR REPLACE FUNCTION reject_payment_webhook_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'payment webhook receipts are append-only';
END $$;
DROP TRIGGER IF EXISTS payment_webhook_receipts_immutable ON payment_webhook_receipts;
CREATE TRIGGER payment_webhook_receipts_immutable
BEFORE UPDATE OR DELETE ON payment_webhook_receipts
FOR EACH ROW EXECUTE FUNCTION reject_payment_webhook_mutation();
