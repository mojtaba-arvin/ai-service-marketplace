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
