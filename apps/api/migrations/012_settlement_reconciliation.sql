CREATE UNIQUE INDEX IF NOT EXISTS payment_operations_external_reference_idx
    ON payment_operations(provider_code,external_reference)
    WHERE external_reference IS NOT NULL;

CREATE TABLE IF NOT EXISTS payment_ledger_links (
    id text PRIMARY KEY,
    operation_id text NOT NULL UNIQUE REFERENCES payment_operations(id) ON DELETE RESTRICT,
    journal_id text NOT NULL UNIQUE REFERENCES ledger_journals(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS settlement_batches (
    id text PRIMARY KEY,
    provider_code text NOT NULL,
    statement_reference text NOT NULL,
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    statement_hash bytea NOT NULL CHECK (octet_length(statement_hash)=32),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(provider_code,statement_reference),
    CHECK (period_end > period_start)
);

CREATE TABLE IF NOT EXISTS settlement_lines (
    id text PRIMARY KEY,
    batch_id text NOT NULL REFERENCES settlement_batches(id) ON DELETE RESTRICT,
    external_reference text NOT NULL,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    settled_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('matched','missing_operation','amount_mismatch','currency_mismatch','missing_ledger','ledger_mismatch')),
    matched_operation_id text REFERENCES payment_operations(id) ON DELETE RESTRICT,
    matched_ledger_journal_id text REFERENCES ledger_journals(id) ON DELETE RESTRICT,
    details text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(batch_id,external_reference)
);
CREATE INDEX IF NOT EXISTS settlement_lines_batch_status_idx ON settlement_lines(batch_id,status);

CREATE OR REPLACE FUNCTION reject_settlement_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'settlement statements and reconciliation results are append-only';
END $$;
DROP TRIGGER IF EXISTS settlement_batches_immutable ON settlement_batches;
CREATE TRIGGER settlement_batches_immutable BEFORE UPDATE OR DELETE ON settlement_batches
FOR EACH ROW EXECUTE FUNCTION reject_settlement_mutation();
DROP TRIGGER IF EXISTS settlement_lines_immutable ON settlement_lines;
CREATE TRIGGER settlement_lines_immutable BEFORE UPDATE OR DELETE ON settlement_lines
FOR EACH ROW EXECUTE FUNCTION reject_settlement_mutation();
DROP TRIGGER IF EXISTS payment_ledger_links_immutable ON payment_ledger_links;
CREATE TRIGGER payment_ledger_links_immutable BEFORE UPDATE OR DELETE ON payment_ledger_links
FOR EACH ROW EXECUTE FUNCTION reject_settlement_mutation();
