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
    lifecycle text NOT NULL CHECK (lifecycle IN ('draft','preview','approved','scheduled','active','retired')),
    provenance text NOT NULL CHECK (length(btrim(provenance)) BETWEEN 1 AND 1000),
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    approved_by_identity_id text REFERENCES phone_identities(id) ON DELETE RESTRICT,
    approval_reason text NOT NULL DEFAULT '',
    approved_at timestamptz,
    CHECK (effective_until IS NULL OR effective_until > effective_from),
    CHECK (maximum_minor IS NULL OR minimum_minor IS NULL OR maximum_minor >= minimum_minor),
    CHECK ((lifecycle IN ('approved','scheduled','active','retired') AND approved_by_identity_id IS NOT NULL AND approved_at IS NOT NULL AND length(btrim(approval_reason)) > 0)
        OR (lifecycle IN ('draft','preview') AND approved_by_identity_id IS NULL AND approved_at IS NULL AND approval_reason='')),
    UNIQUE(market_code,currency,rule_kind,id,version_no)
);
CREATE INDEX IF NOT EXISTS finance_rule_resolution_idx
 ON finance_rule_versions(market_code,currency,rule_kind,lifecycle,effective_from,effective_until,priority DESC);
CREATE UNIQUE INDEX IF NOT EXISTS finance_rule_version_number_idx
 ON finance_rule_versions(market_code,currency,rule_kind,version_no);

-- A rule's definition and approval evidence are immutable. Lifecycle transitions
-- are append-only events; no UPDATE can rewrite an approved historical version.
CREATE TABLE IF NOT EXISTS finance_rule_lifecycle_events (
    id bigserial PRIMARY KEY,
    rule_id text NOT NULL REFERENCES finance_rule_versions(id) ON DELETE RESTRICT,
    from_state text,
    to_state text NOT NULL CHECK (to_state IN ('draft','preview','approved','scheduled','active','retired')),
    actor_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now()
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
    entitlement_state text CHECK (entitlement_state IN ('available','partially_redeemed','redeemed','expired','reversed')),
    ledger_journal_id text REFERENCES ledger_journals(id) ON DELETE RESTRICT,
    actor_identity_id text NOT NULL REFERENCES phone_identities(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(source_kind,source_id,rule_id,rule_version),
    CHECK ((result_kind IN ('reward','credit') AND entitlement_state IS NOT NULL)
        OR (result_kind='commission' AND entitlement_state IS NULL))
);
CREATE INDEX IF NOT EXISTS finance_rule_applications_source_idx
 ON finance_rule_applications(market_code,source_kind,source_id,created_at);

CREATE OR REPLACE FUNCTION reject_finance_rule_version_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'finance rule versions are immutable; publish a new version'; END $$;
DROP TRIGGER IF EXISTS finance_rule_versions_immutable ON finance_rule_versions;
CREATE TRIGGER finance_rule_versions_immutable
 BEFORE UPDATE OR DELETE ON finance_rule_versions FOR EACH ROW EXECUTE FUNCTION reject_finance_rule_version_mutation();

CREATE OR REPLACE FUNCTION reject_finance_rule_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'finance rule lifecycle events are append-only'; END $$;
DROP TRIGGER IF EXISTS finance_rule_lifecycle_events_immutable ON finance_rule_lifecycle_events;
CREATE TRIGGER finance_rule_lifecycle_events_immutable
 BEFORE UPDATE OR DELETE ON finance_rule_lifecycle_events FOR EACH ROW EXECUTE FUNCTION reject_finance_rule_event_mutation();

CREATE OR REPLACE FUNCTION reject_finance_application_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'finance rule applications are immutable; use compensating records'; END $$;
DROP TRIGGER IF EXISTS finance_rule_applications_immutable ON finance_rule_applications;
CREATE TRIGGER finance_rule_applications_immutable
 BEFORE UPDATE OR DELETE ON finance_rule_applications FOR EACH ROW EXECUTE FUNCTION reject_finance_application_mutation();
