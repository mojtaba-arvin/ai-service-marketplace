CREATE TABLE IF NOT EXISTS cancellation_policies (
    id text PRIMARY KEY,
    market_code text NOT NULL REFERENCES markets(code) ON DELETE RESTRICT,
    policy_version text NOT NULL CHECK (length(btrim(policy_version)) BETWEEN 1 AND 80),
    actor_role text NOT NULL CHECK (actor_role='customer'),
    from_status text NOT NULL CHECK (from_status IN ('requested','accepted')),
    cutoff_minutes integer NOT NULL CHECK (cutoff_minutes BETWEEN 0 AND 10080),
    fee_basis_points integer NOT NULL CHECK (fee_basis_points BETWEEN 0 AND 10000),
    effective_from timestamptz NOT NULL,
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(market_code,policy_version,actor_role,from_status)
);

CREATE TABLE IF NOT EXISTS order_cancellations (
    id text PRIMARY KEY,
    order_id text NOT NULL UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
    actor_identity_id text NOT NULL REFERENCES phone_identities(id),
    actor_role text NOT NULL CHECK (actor_role='customer'),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    request_hash bytea NOT NULL CHECK (octet_length(request_hash)=32),
    market_code text NOT NULL,
    policy_version text NOT NULL,
    from_status text NOT NULL,
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    fee_minor bigint NOT NULL CHECK (fee_minor BETWEEN 0 AND amount_minor),
    refund_minor bigint NOT NULL CHECK (refund_minor BETWEEN 0 AND amount_minor),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(actor_identity_id,idempotency_key),
    CHECK (fee_minor+refund_minor=amount_minor),
    FOREIGN KEY(market_code,policy_version,actor_role,from_status)
        REFERENCES cancellation_policies(market_code,policy_version,actor_role,from_status)
);

CREATE INDEX IF NOT EXISTS cancellation_policies_effective_idx
    ON cancellation_policies(market_code,actor_role,from_status,effective_from DESC);
CREATE OR REPLACE FUNCTION reject_cancellation_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'cancellation policies and outcomes are immutable';
END $$;
DROP TRIGGER IF EXISTS cancellation_policies_immutable ON cancellation_policies;
CREATE TRIGGER cancellation_policies_immutable BEFORE UPDATE OR DELETE ON cancellation_policies
FOR EACH ROW EXECUTE FUNCTION reject_cancellation_mutation();
DROP TRIGGER IF EXISTS order_cancellations_immutable ON order_cancellations;
CREATE TRIGGER order_cancellations_immutable BEFORE UPDATE OR DELETE ON order_cancellations
FOR EACH ROW EXECUTE FUNCTION reject_cancellation_mutation();
