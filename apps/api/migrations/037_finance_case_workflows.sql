-- FIN-006: tenant-scoped provider-neutral financial case workflow.
-- This records approvals and provider evidence only; it never initiates a transfer/refund.
ALTER TABLE operator_action_grants
    DROP CONSTRAINT IF EXISTS operator_action_grants_action_check;
ALTER TABLE operator_action_grants
    ADD CONSTRAINT operator_action_grants_action_check CHECK (action IN (
        'order:read','order:assign','provider:verify','case:read','market:configure',
        'catalog:manage','policy:manage','analytics:read','operator:permissions:manage','incident:review','finance:case:manage'
    ));
ALTER TABLE operator_permission_events
    DROP CONSTRAINT IF EXISTS operator_permission_events_action_check;
ALTER TABLE operator_permission_events
    ADD CONSTRAINT operator_permission_events_action_check CHECK (action IN (
        'order:read','order:assign','provider:verify','case:read','market:configure',
        'catalog:manage','policy:manage','analytics:read','operator:permissions:manage',
        'finance:case:manage'
    ));

CREATE UNIQUE INDEX IF NOT EXISTS payment_ledger_links_operation_journal_unique
    ON payment_ledger_links(operation_id,journal_id);

CREATE TABLE IF NOT EXISTS finance_cases (
    id text PRIMARY KEY,
    tenant_id text NOT NULL CHECK (length(btrim(tenant_id)) BETWEEN 1 AND 120),
    case_type text NOT NULL CHECK (case_type IN ('payout','refund','chargeback','payout_reversal')),
    order_id text NOT NULL,
    source_payment_operation_id text NOT NULL REFERENCES payment_operations(id) ON DELETE RESTRICT,
    source_journal_id text NOT NULL REFERENCES ledger_journals(id) ON DELETE RESTRICT,
    provider_code text NOT NULL CHECK (provider_code ~ '^[a-z][a-z0-9_-]{1,40}$'),
    provider_idempotency_key text NOT NULL CHECK (length(btrim(provider_idempotency_key)) BETWEEN 1 AND 200),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    status text NOT NULL DEFAULT 'requested' CHECK (status IN ('requested','accepted','provider_confirmed','failed','reconciled')),
    provider_outcome text NOT NULL DEFAULT 'not_submitted' CHECK (provider_outcome IN ('not_submitted','unknown','confirmed','failed')),
    requested_by_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    request_reason text NOT NULL CHECK (length(btrim(request_reason)) BETWEEN 1 AND 500),
    accepted_by_identity_id text REFERENCES phone_identities(id) ON DELETE RESTRICT,
    acceptance_reason text NOT NULL DEFAULT '',
    failure_code text NOT NULL DEFAULT '',
    reversal_of_case_id text,
    reconciled_journal_id text REFERENCES ledger_journals(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(provider_code,provider_idempotency_key),
    UNIQUE(id,tenant_id),
    FOREIGN KEY(order_id,tenant_id) REFERENCES orders(id,tenant_id) ON DELETE RESTRICT,
    FOREIGN KEY(reversal_of_case_id,tenant_id) REFERENCES finance_cases(id,tenant_id) ON DELETE RESTRICT,
    CHECK ((case_type='payout_reversal' AND reversal_of_case_id IS NOT NULL) OR
           (case_type<>'payout_reversal' AND reversal_of_case_id IS NULL)),
    CHECK ((status='reconciled' AND reconciled_journal_id IS NOT NULL) OR
           (status<>'reconciled' AND reconciled_journal_id IS NULL)),
    CHECK ((accepted_by_identity_id IS NULL AND acceptance_reason='') OR
           (accepted_by_identity_id IS NOT NULL AND length(btrim(acceptance_reason)) BETWEEN 1 AND 500)),
    CHECK ((status IN ('provider_confirmed','reconciled') AND provider_outcome='confirmed') OR
           (status='failed' AND provider_outcome='failed') OR
           (status IN ('requested','accepted') AND provider_outcome IN ('not_submitted','unknown')))
);
CREATE UNIQUE INDEX IF NOT EXISTS finance_cases_one_reversal_per_case
    ON finance_cases(reversal_of_case_id) WHERE reversal_of_case_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS finance_cases_tenant_status_idx
    ON finance_cases(tenant_id,status,created_at DESC);
CREATE INDEX IF NOT EXISTS finance_cases_order_idx ON finance_cases(tenant_id,order_id,created_at);

CREATE TABLE IF NOT EXISTS finance_case_events (
    id text PRIMARY KEY,
    case_id text NOT NULL REFERENCES finance_cases(id) ON DELETE RESTRICT,
    tenant_id text NOT NULL,
    event_type text NOT NULL CHECK (event_type IN
        ('requested','accepted','provider_unknown','provider_confirmed','failed','reconciled')),
    from_status text,
    to_status text NOT NULL,
    actor_identity_id text REFERENCES phone_identities(id) ON DELETE RESTRICT,
    reason text NOT NULL DEFAULT '',
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(case_id,tenant_id) REFERENCES finance_cases(id,tenant_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS finance_case_events_case_idx
    ON finance_case_events(tenant_id,case_id,created_at,id);

CREATE TABLE IF NOT EXISTS finance_case_webhook_receipts (
    id text PRIMARY KEY,
    provider_code text NOT NULL,
    event_id text NOT NULL,
    case_id text NOT NULL REFERENCES finance_cases(id) ON DELETE RESTRICT,
    tenant_id text NOT NULL,
    payload_hash bytea NOT NULL CHECK (octet_length(payload_hash)=32),
    received_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(provider_code,event_id),
    FOREIGN KEY(case_id,tenant_id) REFERENCES finance_cases(id,tenant_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS finance_case_reconciliation_checks (
    id text PRIMARY KEY,
    case_id text NOT NULL,
    tenant_id text NOT NULL,
    journal_id text,
    outcome text NOT NULL CHECK (outcome IN
        ('matched','missing_journal','amount_mismatch','currency_mismatch','reversal_mismatch')),
    expected_amount_minor bigint NOT NULL CHECK (expected_amount_minor > 0),
    expected_currency char(3) NOT NULL CHECK (expected_currency ~ '^[A-Z]{3}
    id text PRIMARY KEY,
    case_id text NOT NULL UNIQUE REFERENCES finance_cases(id) ON DELETE RESTRICT,
    journal_id text NOT NULL UNIQUE REFERENCES ledger_journals(id) ON DELETE RESTRICT,
    tenant_id text NOT NULL,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(case_id,tenant_id) REFERENCES finance_cases(id,tenant_id) ON DELETE RESTRICT
);

CREATE OR REPLACE FUNCTION reject_finance_case_evidence_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'finance case events, webhook receipts and ledger links are append-only';
END $$;
DROP TRIGGER IF EXISTS finance_case_events_immutable ON finance_case_events;
CREATE TRIGGER finance_case_events_immutable BEFORE UPDATE OR DELETE ON finance_case_events
FOR EACH ROW EXECUTE FUNCTION reject_finance_case_evidence_mutation();
DROP TRIGGER IF EXISTS finance_case_webhook_receipts_immutable ON finance_case_webhook_receipts;
CREATE TRIGGER finance_case_webhook_receipts_immutable BEFORE UPDATE OR DELETE ON finance_case_webhook_receipts
FOR EACH ROW EXECUTE FUNCTION reject_finance_case_evidence_mutation();
DROP TRIGGER IF EXISTS finance_case_reconciliation_checks_immutable ON finance_case_reconciliation_checks;
CREATE TRIGGER finance_case_reconciliation_checks_immutable BEFORE UPDATE OR DELETE ON finance_case_reconciliation_checks
FOR EACH ROW EXECUTE FUNCTION reject_finance_case_evidence_mutation();
DROP TRIGGER IF EXISTS finance_case_ledger_links_immutable ON finance_case_ledger_links;
CREATE TRIGGER finance_case_ledger_links_immutable BEFORE UPDATE OR DELETE ON finance_case_ledger_links
FOR EACH ROW EXECUTE FUNCTION reject_finance_case_evidence_mutation();

CREATE OR REPLACE FUNCTION guard_finance_case_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.case_type IS DISTINCT FROM OLD.case_type OR NEW.order_id IS DISTINCT FROM OLD.order_id
       OR NEW.source_payment_operation_id IS DISTINCT FROM OLD.source_payment_operation_id
       OR NEW.source_journal_id IS DISTINCT FROM OLD.source_journal_id
       OR NEW.provider_code IS DISTINCT FROM OLD.provider_code
       OR NEW.provider_idempotency_key IS DISTINCT FROM OLD.provider_idempotency_key
       OR NEW.amount_minor IS DISTINCT FROM OLD.amount_minor OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.requested_by_identity_id IS DISTINCT FROM OLD.requested_by_identity_id
       OR NEW.reversal_of_case_id IS DISTINCT FROM OLD.reversal_of_case_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'finance case request provenance is immutable';
    END IF;
    IF NEW.status IN ('accepted','provider_confirmed','reconciled')
       AND (NEW.accepted_by_identity_id IS NULL OR length(btrim(NEW.acceptance_reason)) NOT BETWEEN 1 AND 500) THEN
        RAISE EXCEPTION 'financial case requires explicit human acceptance';
    END IF;
    IF OLD.status='requested' AND NEW.status IN ('requested','accepted','failed') THEN RETURN NEW; END IF;
    IF OLD.status='accepted' AND NEW.status IN ('accepted','provider_confirmed','failed') THEN RETURN NEW; END IF;
    IF OLD.status='provider_confirmed' AND NEW.status IN ('provider_confirmed','reconciled') THEN RETURN NEW; END IF;
    RAISE EXCEPTION 'invalid finance case state transition';
END $$;
DROP TRIGGER IF EXISTS finance_case_state_guard ON finance_cases;
CREATE TRIGGER finance_case_state_guard BEFORE UPDATE ON finance_cases
FOR EACH ROW EXECUTE FUNCTION guard_finance_case_transition();
),
    actual_amount_minor bigint NOT NULL CHECK (actual_amount_minor >= 0),
    actual_currency char(3),
    actor_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(case_id,tenant_id) REFERENCES finance_cases(id,tenant_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS finance_case_reconciliation_case_idx
    ON finance_case_reconciliation_checks(tenant_id,case_id,created_at DESC);

CREATE TABLE IF NOT EXISTS finance_case_ledger_links (
    id text PRIMARY KEY,
    case_id text NOT NULL UNIQUE REFERENCES finance_cases(id) ON DELETE RESTRICT,
    journal_id text NOT NULL UNIQUE REFERENCES ledger_journals(id) ON DELETE RESTRICT,
    tenant_id text NOT NULL,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(case_id,tenant_id) REFERENCES finance_cases(id,tenant_id) ON DELETE RESTRICT
);

CREATE OR REPLACE FUNCTION reject_finance_case_evidence_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'finance case events, webhook receipts and ledger links are append-only';
END $$;
DROP TRIGGER IF EXISTS finance_case_events_immutable ON finance_case_events;
CREATE TRIGGER finance_case_events_immutable BEFORE UPDATE OR DELETE ON finance_case_events
FOR EACH ROW EXECUTE FUNCTION reject_finance_case_evidence_mutation();
DROP TRIGGER IF EXISTS finance_case_webhook_receipts_immutable ON finance_case_webhook_receipts;
CREATE TRIGGER finance_case_webhook_receipts_immutable BEFORE UPDATE OR DELETE ON finance_case_webhook_receipts
FOR EACH ROW EXECUTE FUNCTION reject_finance_case_evidence_mutation();
DROP TRIGGER IF EXISTS finance_case_ledger_links_immutable ON finance_case_ledger_links;
CREATE TRIGGER finance_case_ledger_links_immutable BEFORE UPDATE OR DELETE ON finance_case_ledger_links
FOR EACH ROW EXECUTE FUNCTION reject_finance_case_evidence_mutation();

CREATE OR REPLACE FUNCTION guard_finance_case_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
       OR NEW.case_type IS DISTINCT FROM OLD.case_type OR NEW.order_id IS DISTINCT FROM OLD.order_id
       OR NEW.source_payment_operation_id IS DISTINCT FROM OLD.source_payment_operation_id
       OR NEW.source_journal_id IS DISTINCT FROM OLD.source_journal_id
       OR NEW.provider_code IS DISTINCT FROM OLD.provider_code
       OR NEW.provider_idempotency_key IS DISTINCT FROM OLD.provider_idempotency_key
       OR NEW.amount_minor IS DISTINCT FROM OLD.amount_minor OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.requested_by_identity_id IS DISTINCT FROM OLD.requested_by_identity_id
       OR NEW.reversal_of_case_id IS DISTINCT FROM OLD.reversal_of_case_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'finance case request provenance is immutable';
    END IF;
    IF NEW.status IN ('accepted','provider_confirmed','reconciled')
       AND (NEW.accepted_by_identity_id IS NULL OR length(btrim(NEW.acceptance_reason)) NOT BETWEEN 1 AND 500) THEN
        RAISE EXCEPTION 'financial case requires explicit human acceptance';
    END IF;
    IF OLD.status='requested' AND NEW.status IN ('requested','accepted','failed') THEN RETURN NEW; END IF;
    IF OLD.status='accepted' AND NEW.status IN ('accepted','provider_confirmed','failed') THEN RETURN NEW; END IF;
    IF OLD.status='provider_confirmed' AND NEW.status IN ('provider_confirmed','reconciled') THEN RETURN NEW; END IF;
    RAISE EXCEPTION 'invalid finance case state transition';
END $$;
DROP TRIGGER IF EXISTS finance_case_state_guard ON finance_cases;
CREATE TRIGGER finance_case_state_guard BEFORE UPDATE ON finance_cases
FOR EACH ROW EXECUTE FUNCTION guard_finance_case_transition();
