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
