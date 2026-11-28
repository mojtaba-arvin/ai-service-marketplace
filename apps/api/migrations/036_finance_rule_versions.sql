-- FIN-008: immutable, adopter-approved market/currency rule versions and application snapshots.
-- This migration defines storage only. It seeds no rates and activates no payment provider.
CREATE TABLE IF NOT EXISTS finance_rule_versions (
    id text PRIMARY KEY CHECK (length(btrim(id)) BETWEEN 1 AND 120),
    version_no integer NOT NULL CHECK (version_no > 0),
    market_code text NOT NULL REFERENCES markets(code) ON DELETE RESTRICT,
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    rule_kind text NOT NULL CHECK (rule_kind IN ('commission','reward','credit')),
    calculation_basis text NOT NULL CHECK (calculation_basis IN ('fixed_minor','percentage')),
    rate_minor_or_bps bigint NOT NULL CHECK (rate_minor_or_bps >= 0),
    rounding_mode text NOT NULL CHECK (rounding_mode IN ('half_up','down','up')),
    effective_from timestamptz NOT NULL,
    effective_until timestamptz,
    priority integer NOT NULL DEFAULT 0,
    minimum_minor bigint CHECK (minimum_minor >= 0),
    maximum_minor bigint CHECK (maximum_minor >= 0),
    provenance text NOT NULL CHECK (length(btrim(provenance)) BETWEEN 1 AND 1000),
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (effective_until IS NULL OR effective_until > effective_from),
    CHECK (maximum_minor IS NULL OR minimum_minor IS NULL OR maximum_minor >= minimum_minor),
    CHECK (calculation_basis <> 'percentage' OR rate_minor_or_bps <= 10000),
    UNIQUE(market_code,currency,rule_kind,version_no)
);
CREATE INDEX IF NOT EXISTS finance_rule_resolution_idx
 ON finance_rule_versions(market_code,currency,rule_kind,effective_from,effective_until,priority DESC);

-- State is a projection of this append-only event stream, never a mutable field
-- on the immutable rule definition. The first event must be draft.
CREATE TABLE IF NOT EXISTS finance_rule_lifecycle_events (
    id bigserial PRIMARY KEY,
    rule_id text NOT NULL REFERENCES finance_rule_versions(id) ON DELETE RESTRICT,
    from_state text,
    to_state text NOT NULL CHECK (to_state IN ('draft','preview','approved','scheduled','active','retired')),
    actor_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    approval_reference text NOT NULL DEFAULT '',
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((to_state='approved' AND length(btrim(approval_reference))>0) OR to_state<>'approved')
);
CREATE INDEX IF NOT EXISTS finance_rule_lifecycle_events_rule_idx
 ON finance_rule_lifecycle_events(rule_id,created_at,id);

CREATE TABLE IF NOT EXISTS finance_rule_applications (
    id text PRIMARY KEY,
    idempotency_key text NOT NULL UNIQUE CHECK (length(btrim(idempotency_key)) BETWEEN 1 AND 200),
    market_code text NOT NULL REFERENCES markets(code) ON DELETE RESTRICT,
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    rule_id text NOT NULL REFERENCES finance_rule_versions(id) ON DELETE RESTRICT,
    rule_version integer NOT NULL CHECK (rule_version > 0),
    rule_snapshot jsonb NOT NULL,
    input_snapshot jsonb NOT NULL,
    input_sha256 char(64) NOT NULL CHECK (input_sha256 ~ '^[0-9a-f]{64}$'),
    source_kind text NOT NULL CHECK (source_kind IN ('order','attribution','operator_adjustment')),
    source_id text NOT NULL CHECK (length(btrim(source_id)) BETWEEN 1 AND 200),
    result_minor bigint NOT NULL CHECK (result_minor >= 0),
    result_kind text NOT NULL CHECK (result_kind IN ('commission','reward','credit')),
    ledger_journal_id text REFERENCES ledger_journals(id) ON DELETE RESTRICT,
    actor_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    reversal_of_application_id text REFERENCES finance_rule_applications(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(source_kind,source_id,rule_id,rule_version),
    CHECK (reversal_of_application_id IS NULL OR reversal_of_application_id <> id)
);
CREATE UNIQUE INDEX IF NOT EXISTS finance_rule_application_one_reversal_idx
 ON finance_rule_applications(reversal_of_application_id) WHERE reversal_of_application_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS finance_rule_applications_source_idx
 ON finance_rule_applications(market_code,source_kind,source_id,created_at);

-- Reward and credit value is a non-cash entitlement. Its state changes only
-- through append-only events and cannot be mistaken for a cash ledger balance.
CREATE TABLE IF NOT EXISTS finance_entitlement_events (
    id bigserial PRIMARY KEY,
    application_id text NOT NULL REFERENCES finance_rule_applications(id) ON DELETE RESTRICT,
    event_type text NOT NULL CHECK (event_type IN ('issued','partially_redeemed','redeemed','expired','reversed')),
    amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
    actor_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS finance_entitlement_events_application_idx
 ON finance_entitlement_events(application_id,created_at,id);

CREATE OR REPLACE FUNCTION reject_finance_rule_version_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'finance rule versions are immutable; publish a new version'; END $$;
DROP TRIGGER IF EXISTS finance_rule_versions_immutable ON finance_rule_versions;
CREATE TRIGGER finance_rule_versions_immutable
 BEFORE UPDATE OR DELETE ON finance_rule_versions FOR EACH ROW EXECUTE FUNCTION reject_finance_rule_version_mutation();

CREATE OR REPLACE FUNCTION reject_finance_append_only_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'finance evidence is append-only; add a compensating event'; END $$;
DROP TRIGGER IF EXISTS finance_rule_lifecycle_events_immutable ON finance_rule_lifecycle_events;
CREATE TRIGGER finance_rule_lifecycle_events_immutable
 BEFORE UPDATE OR DELETE ON finance_rule_lifecycle_events FOR EACH ROW EXECUTE FUNCTION reject_finance_append_only_mutation();
DROP TRIGGER IF EXISTS finance_rule_applications_immutable ON finance_rule_applications;
CREATE TRIGGER finance_rule_applications_immutable
 BEFORE UPDATE OR DELETE ON finance_rule_applications FOR EACH ROW EXECUTE FUNCTION reject_finance_append_only_mutation();
DROP TRIGGER IF EXISTS finance_entitlement_events_immutable ON finance_entitlement_events;
CREATE TRIGGER finance_entitlement_events_immutable
 BEFORE UPDATE OR DELETE ON finance_entitlement_events FOR EACH ROW EXECUTE FUNCTION reject_finance_append_only_mutation();
