CREATE TABLE IF NOT EXISTS orders (
    id text PRIMARY KEY,
    customer_id text NOT NULL,
    idempotency_key text NOT NULL,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status text NOT NULL CHECK (status IN ('requested','accepted','in_progress','completed','cancelled','disputed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(customer_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS ledger_journals (
    id text PRIMARY KEY,
    idempotency_key text NOT NULL UNIQUE,
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    description text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id bigserial PRIMARY KEY,
    journal_id text NOT NULL REFERENCES ledger_journals(id),
    account_code text NOT NULL,
    side text NOT NULL CHECK (side IN ('debit','credit')),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ledger_entries_journal_idx ON ledger_entries(journal_id);

CREATE TABLE IF NOT EXISTS outbox_events (
    id bigserial PRIMARY KEY,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);
CREATE INDEX IF NOT EXISTS outbox_pending_idx ON outbox_events(id) WHERE published_at IS NULL;

-- Deferred integrity check: every committed journal must contain balanced debits and credits.
CREATE OR REPLACE FUNCTION enforce_balanced_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_id text;
    debit_total bigint;
    credit_total bigint;
    entry_count bigint;
BEGIN
    target_id := COALESCE(NEW.journal_id, OLD.journal_id);
    SELECT COUNT(*),
           COALESCE(SUM(amount_minor) FILTER (WHERE side='debit'),0),
           COALESCE(SUM(amount_minor) FILTER (WHERE side='credit'),0)
      INTO entry_count, debit_total, credit_total
      FROM ledger_entries WHERE journal_id=target_id;
    IF entry_count < 2 OR debit_total <> credit_total THEN
        RAISE EXCEPTION 'journal % is not balanced', target_id;
    END IF;
    RETURN NULL;
END $$;

DROP TRIGGER IF EXISTS ledger_journal_balance_check ON ledger_entries;
CREATE CONSTRAINT TRIGGER ledger_journal_balance_check
AFTER INSERT ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_balanced_journal();

CREATE OR REPLACE FUNCTION reject_ledger_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger records are append-only; post a compensating journal';
END $$;
DROP TRIGGER IF EXISTS ledger_entries_immutable ON ledger_entries;
CREATE TRIGGER ledger_entries_immutable
BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
DROP TRIGGER IF EXISTS ledger_journals_immutable ON ledger_journals;
CREATE TRIGGER ledger_journals_immutable
BEFORE UPDATE OR DELETE ON ledger_journals
FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE IF NOT EXISTS processed_events (
    consumer_name text NOT NULL,
    event_id bigint NOT NULL REFERENCES outbox_events(id),
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(consumer_name,event_id)
);
