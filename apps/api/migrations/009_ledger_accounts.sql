CREATE TABLE IF NOT EXISTS ledger_accounts (
    id text PRIMARY KEY,
    account_code text NOT NULL CHECK (account_code ~ '^[a-z][a-z0-9_.-]{1,63}$'),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    normal_balance text NOT NULL CHECK (normal_balance IN ('debit','credit')),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(account_code,currency)
);
CREATE INDEX IF NOT EXISTS ledger_accounts_currency_enabled_idx
    ON ledger_accounts(currency,enabled,account_code);
