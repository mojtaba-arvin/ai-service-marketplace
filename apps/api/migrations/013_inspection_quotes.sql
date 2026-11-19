CREATE TABLE IF NOT EXISTS inspection_quotes (
    id text PRIMARY KEY,
    order_id text NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    provider_id text NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
    revision_no integer NOT NULL CHECK (revision_no > 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    scope text NOT NULL CHECK (length(btrim(scope)) BETWEEN 1 AND 4000),
    total_minor bigint NOT NULL CHECK (total_minor > 0),
    expires_at timestamptz NOT NULL,
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(order_id,revision_no)
);

CREATE TABLE IF NOT EXISTS quote_line_items (
    id text PRIMARY KEY,
    quote_id text NOT NULL REFERENCES inspection_quotes(id) ON DELETE RESTRICT,
    line_code text NOT NULL CHECK (line_code ~ '^[a-z][a-z0-9_-]{1,63}$'),
    description text NOT NULL CHECK (length(btrim(description)) BETWEEN 1 AND 500),
    quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 10000),
    unit_amount_minor bigint NOT NULL CHECK (unit_amount_minor > 0),
    line_total_minor bigint NOT NULL CHECK (line_total_minor > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(quote_id,line_code)
);

CREATE TABLE IF NOT EXISTS quote_evidence_refs (
    id text PRIMARY KEY,
    quote_id text NOT NULL REFERENCES inspection_quotes(id) ON DELETE RESTRICT,
    evidence_kind text NOT NULL CHECK (evidence_kind IN ('photo','document','measurement','other')),
    evidence_reference text NOT NULL CHECK (evidence_reference LIKE 'evidence://%' AND length(evidence_reference) <= 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(quote_id,evidence_reference)
);

CREATE TABLE IF NOT EXISTS quote_decisions (
    id text PRIMARY KEY,
    quote_id text NOT NULL REFERENCES inspection_quotes(id) ON DELETE RESTRICT,
    actor_identity_id text NOT NULL REFERENCES phone_identities(id),
    actor_role text NOT NULL CHECK (actor_role IN ('customer','professional','operator')),
    action text NOT NULL CHECK (action IN ('approved','rejected','accepted')),
    reason text NOT NULL DEFAULT '' CHECK (length(reason) <= 1000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS quote_decisions_latest_idx ON quote_decisions(quote_id,created_at DESC,id DESC);

CREATE OR REPLACE FUNCTION reject_quote_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'quote revisions, line items, evidence references, and decisions are immutable';
END $$;
DROP TRIGGER IF EXISTS inspection_quotes_immutable ON inspection_quotes;
CREATE TRIGGER inspection_quotes_immutable BEFORE UPDATE OR DELETE ON inspection_quotes
FOR EACH ROW EXECUTE FUNCTION reject_quote_mutation();
DROP TRIGGER IF EXISTS quote_line_items_immutable ON quote_line_items;
CREATE TRIGGER quote_line_items_immutable BEFORE UPDATE OR DELETE ON quote_line_items
FOR EACH ROW EXECUTE FUNCTION reject_quote_mutation();
DROP TRIGGER IF EXISTS quote_evidence_refs_immutable ON quote_evidence_refs;
CREATE TRIGGER quote_evidence_refs_immutable BEFORE UPDATE OR DELETE ON quote_evidence_refs
FOR EACH ROW EXECUTE FUNCTION reject_quote_mutation();
DROP TRIGGER IF EXISTS quote_decisions_immutable ON quote_decisions;
CREATE TRIGGER quote_decisions_immutable BEFORE UPDATE OR DELETE ON quote_decisions
FOR EACH ROW EXECUTE FUNCTION reject_quote_mutation();
