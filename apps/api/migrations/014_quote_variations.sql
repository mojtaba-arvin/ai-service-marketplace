CREATE TABLE IF NOT EXISTS quote_variations (
    id text PRIMARY KEY,
    quote_id text NOT NULL REFERENCES inspection_quotes(id) ON DELETE RESTRICT,
    revision_no integer NOT NULL CHECK (revision_no > 0),
    base_amount_minor bigint NOT NULL CHECK (base_amount_minor > 0),
    amount_delta_minor bigint NOT NULL CHECK (amount_delta_minor <> 0),
    proposed_total_minor bigint NOT NULL CHECK (proposed_total_minor > 0),
    scope_delta text NOT NULL CHECK (length(btrim(scope_delta)) BETWEEN 1 AND 2000),
    expires_at timestamptz NOT NULL,
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(quote_id,revision_no)
);

CREATE TABLE IF NOT EXISTS quote_variation_decisions (
    id text PRIMARY KEY,
    variation_id text NOT NULL REFERENCES quote_variations(id) ON DELETE RESTRICT,
    actor_identity_id text NOT NULL REFERENCES phone_identities(id),
    actor_role text NOT NULL CHECK (actor_role='customer'),
    action text NOT NULL CHECK (action IN ('approved','rejected')),
    reason text NOT NULL DEFAULT '' CHECK (length(reason)<=1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(variation_id)
);

CREATE TABLE IF NOT EXISTS order_price_revisions (
    id text PRIMARY KEY,
    order_id text NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    revision_no integer NOT NULL CHECK (revision_no > 0),
    source_type text NOT NULL CHECK (source_type IN ('quote','variation')),
    source_id text NOT NULL,
    previous_amount_minor bigint NOT NULL CHECK (previous_amount_minor > 0),
    new_amount_minor bigint NOT NULL CHECK (new_amount_minor > 0),
    approved_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(order_id,revision_no),
    UNIQUE(source_type,source_id)
);
CREATE INDEX IF NOT EXISTS quote_variations_quote_idx ON quote_variations(quote_id,revision_no DESC);

CREATE OR REPLACE FUNCTION validate_order_price_revision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    current_amount bigint;
    source_order_id text;
    approved boolean;
    expected_amount bigint;
BEGIN
    SELECT amount_minor INTO current_amount FROM orders WHERE id=NEW.order_id FOR UPDATE;
    IF current_amount IS NULL OR current_amount<>NEW.previous_amount_minor THEN
        RAISE EXCEPTION 'price revision base amount does not match current order amount';
    END IF;
    IF NEW.source_type='quote' THEN
        SELECT q.order_id,q.total_minor,
            EXISTS(SELECT 1 FROM quote_decisions d WHERE d.quote_id=q.id AND d.action='approved' AND d.actor_identity_id=NEW.approved_by_identity_id)
        INTO source_order_id,expected_amount,approved
        FROM inspection_quotes q WHERE q.id=NEW.source_id;
    ELSE
        SELECT q.order_id,v.proposed_total_minor,
            EXISTS(SELECT 1 FROM quote_variation_decisions d WHERE d.variation_id=v.id AND d.action='approved' AND d.actor_identity_id=NEW.approved_by_identity_id)
        INTO source_order_id,expected_amount,approved
        FROM quote_variations v JOIN inspection_quotes q ON q.id=v.quote_id WHERE v.id=NEW.source_id;
    END IF;
    IF source_order_id IS NULL OR source_order_id<>NEW.order_id OR expected_amount<>NEW.new_amount_minor OR NOT approved THEN
        RAISE EXCEPTION 'order price revision requires matching customer approval';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS order_price_revision_approval_guard ON order_price_revisions;
CREATE TRIGGER order_price_revision_approval_guard
BEFORE INSERT ON order_price_revisions
FOR EACH ROW EXECUTE FUNCTION validate_order_price_revision();

CREATE OR REPLACE FUNCTION guard_order_amount_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.amount_minor IS DISTINCT FROM OLD.amount_minor AND NOT EXISTS (
        SELECT 1 FROM order_price_revisions r
        WHERE r.order_id=OLD.id AND r.previous_amount_minor=OLD.amount_minor
          AND r.new_amount_minor=NEW.amount_minor AND r.created_at>=OLD.updated_at
    ) THEN
        RAISE EXCEPTION 'order amount changes require an approved immutable price revision';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS orders_price_revision_guard ON orders;
CREATE TRIGGER orders_price_revision_guard
BEFORE UPDATE OF amount_minor ON orders
FOR EACH ROW EXECUTE FUNCTION guard_order_amount_change();

CREATE OR REPLACE FUNCTION reject_price_revision_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'quote variations, decisions, and order price revisions are immutable';
END $$;
DROP TRIGGER IF EXISTS quote_variations_immutable ON quote_variations;
CREATE TRIGGER quote_variations_immutable BEFORE UPDATE OR DELETE ON quote_variations
FOR EACH ROW EXECUTE FUNCTION reject_price_revision_mutation();
DROP TRIGGER IF EXISTS quote_variation_decisions_immutable ON quote_variation_decisions;
CREATE TRIGGER quote_variation_decisions_immutable BEFORE UPDATE OR DELETE ON quote_variation_decisions
FOR EACH ROW EXECUTE FUNCTION reject_price_revision_mutation();
DROP TRIGGER IF EXISTS order_price_revisions_immutable ON order_price_revisions;
CREATE TRIGGER order_price_revisions_immutable BEFORE UPDATE OR DELETE ON order_price_revisions
FOR EACH ROW EXECUTE FUNCTION reject_price_revision_mutation();

CREATE OR REPLACE FUNCTION validate_quote_decision_actor() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    expected_identity text;
BEGIN
    IF NEW.action IN ('approved','rejected') THEN
        SELECT o.customer_id INTO expected_identity
        FROM inspection_quotes q JOIN orders o ON o.id=q.order_id WHERE q.id=NEW.quote_id;
        IF expected_identity IS NULL OR expected_identity<>NEW.actor_identity_id OR NEW.actor_role<>'customer' THEN
            RAISE EXCEPTION 'quote approval/rejection must be made by the owning customer';
        END IF;
    ELSE
        SELECT p.identity_id INTO expected_identity
        FROM inspection_quotes q JOIN provider_profiles p ON p.id=q.provider_id WHERE q.id=NEW.quote_id;
        IF expected_identity IS NULL OR expected_identity<>NEW.actor_identity_id OR NEW.actor_role<>'professional' THEN
            RAISE EXCEPTION 'quote acceptance must be made by its professional';
        END IF;
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS quote_decision_actor_guard ON quote_decisions;
CREATE TRIGGER quote_decision_actor_guard BEFORE INSERT ON quote_decisions
FOR EACH ROW EXECUTE FUNCTION validate_quote_decision_actor();

CREATE OR REPLACE FUNCTION validate_quote_variation_decision_actor() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    expected_identity text;
BEGIN
    SELECT o.customer_id INTO expected_identity
    FROM quote_variations v
    JOIN inspection_quotes q ON q.id=v.quote_id
    JOIN orders o ON o.id=q.order_id
    WHERE v.id=NEW.variation_id;
    IF expected_identity IS NULL OR expected_identity<>NEW.actor_identity_id OR NEW.actor_role<>'customer' THEN
        RAISE EXCEPTION 'price variation decision must be made by the owning customer';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS quote_variation_decision_actor_guard ON quote_variation_decisions;
CREATE TRIGGER quote_variation_decision_actor_guard BEFORE INSERT ON quote_variation_decisions
FOR EACH ROW EXECUTE FUNCTION validate_quote_variation_decision_actor();
