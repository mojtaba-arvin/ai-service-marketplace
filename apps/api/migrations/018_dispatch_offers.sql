ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS assigned_provider_id text REFERENCES provider_profiles(id) ON DELETE RESTRICT;

CREATE TABLE IF NOT EXISTS dispatch_rounds (
    id text PRIMARY KEY,
    order_id text NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    round_no integer NOT NULL CHECK (round_no>0),
    fanout_limit integer NOT NULL CHECK (fanout_limit BETWEEN 1 AND 10),
    offer_count integer NOT NULL CHECK (offer_count BETWEEN 1 AND fanout_limit),
    expires_at timestamptz NOT NULL,
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(order_id,round_no)
);

CREATE TABLE IF NOT EXISTS dispatch_offers (
    id text PRIMARY KEY,
    round_id text NOT NULL REFERENCES dispatch_rounds(id) ON DELETE RESTRICT,
    order_id text NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    provider_id text NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    timezone text NOT NULL,
    status text NOT NULL DEFAULT 'offered' CHECK (status IN ('offered','accepted','declined','expired','superseded')),
    expires_at timestamptz NOT NULL,
    decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (end_at>start_at),
    UNIQUE(round_id,provider_id)
);
CREATE INDEX IF NOT EXISTS dispatch_offers_pending_idx ON dispatch_offers(order_id,expires_at) WHERE status='offered';
CREATE INDEX IF NOT EXISTS dispatch_offers_provider_idx ON dispatch_offers(provider_id,status,expires_at);

CREATE TABLE IF NOT EXISTS dispatch_assignments (
    id text PRIMARY KEY,
    order_id text NOT NULL UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
    provider_id text NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
    offer_id text NOT NULL UNIQUE REFERENCES dispatch_offers(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION guard_dispatch_offer_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'dispatch offers cannot be deleted'; END IF;
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.round_id IS DISTINCT FROM OLD.round_id
       OR NEW.order_id IS DISTINCT FROM OLD.order_id OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.start_at IS DISTINCT FROM OLD.start_at OR NEW.end_at IS DISTINCT FROM OLD.end_at
       OR NEW.timezone IS DISTINCT FROM OLD.timezone OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'dispatch offer details are immutable';
    END IF;
    IF OLD.status='offered' AND NEW.status IN ('accepted','declined','expired','superseded') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'invalid dispatch offer transition';
END $$;
DROP TRIGGER IF EXISTS dispatch_offers_state_guard ON dispatch_offers;
CREATE TRIGGER dispatch_offers_state_guard BEFORE UPDATE OR DELETE ON dispatch_offers
FOR EACH ROW EXECUTE FUNCTION guard_dispatch_offer_transition();

CREATE OR REPLACE FUNCTION validate_dispatch_assignment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    offer_order text;
    offer_provider text;
    offer_status text;
BEGIN
    SELECT order_id,provider_id,status INTO offer_order,offer_provider,offer_status
    FROM dispatch_offers WHERE id=NEW.offer_id;
    IF offer_order IS NULL OR offer_order<>NEW.order_id OR offer_provider<>NEW.provider_id OR offer_status<>'accepted' THEN
        RAISE EXCEPTION 'assignment must reference its accepted offer';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS dispatch_assignment_offer_guard ON dispatch_assignments;
CREATE TRIGGER dispatch_assignment_offer_guard BEFORE INSERT ON dispatch_assignments
FOR EACH ROW EXECUTE FUNCTION validate_dispatch_assignment();

CREATE OR REPLACE FUNCTION guard_order_provider_assignment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.assigned_provider_id IS NOT NULL AND NEW.assigned_provider_id IS DISTINCT FROM OLD.assigned_provider_id THEN
        RAISE EXCEPTION 'provider assignment is immutable once accepted';
    END IF;
    IF NEW.assigned_provider_id IS DISTINCT FROM OLD.assigned_provider_id AND NOT EXISTS (
        SELECT 1 FROM dispatch_assignments a WHERE a.order_id=OLD.id AND a.provider_id=NEW.assigned_provider_id
    ) THEN
        RAISE EXCEPTION 'order provider assignment requires a unique dispatch assignment';
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS orders_provider_assignment_guard ON orders;
CREATE TRIGGER orders_provider_assignment_guard BEFORE UPDATE OF assigned_provider_id ON orders
FOR EACH ROW EXECUTE FUNCTION guard_order_provider_assignment();

CREATE OR REPLACE FUNCTION reject_dispatch_assignment_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'dispatch assignments are immutable';
END $$;
DROP TRIGGER IF EXISTS dispatch_assignments_immutable ON dispatch_assignments;
CREATE TRIGGER dispatch_assignments_immutable BEFORE UPDATE OR DELETE ON dispatch_assignments
FOR EACH ROW EXECUTE FUNCTION reject_dispatch_assignment_mutation();
