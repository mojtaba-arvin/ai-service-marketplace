CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE IF NOT EXISTS provider_availability_windows (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
    window_type text NOT NULL CHECK (window_type IN ('available','unavailable')),
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    timezone text NOT NULL CHECK (length(btrim(timezone)) BETWEEN 1 AND 80),
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (end_at>start_at),
    CHECK (end_at-start_at<=interval '31 days')
);

ALTER TABLE provider_availability_windows
    DROP CONSTRAINT IF EXISTS provider_availability_no_overlap;
ALTER TABLE provider_availability_windows
    ADD CONSTRAINT provider_availability_no_overlap
    EXCLUDE USING gist (
        provider_id WITH =,
        tstzrange(start_at,end_at,'[)') WITH &&
    ) WHERE (window_type='available');

CREATE TABLE IF NOT EXISTS provider_bookings (
    id text PRIMARY KEY,
    provider_id text NOT NULL REFERENCES provider_profiles(id) ON DELETE RESTRICT,
    order_id text NOT NULL UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
    start_at timestamptz NOT NULL,
    end_at timestamptz NOT NULL,
    timezone text NOT NULL CHECK (length(btrim(timezone)) BETWEEN 1 AND 80),
    status text NOT NULL DEFAULT 'booked' CHECK (status IN ('booked','cancelled')),
    created_by_identity_id text NOT NULL REFERENCES phone_identities(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (end_at>start_at),
    CHECK (end_at-start_at<=interval '24 hours')
);

ALTER TABLE provider_bookings
    DROP CONSTRAINT IF EXISTS provider_bookings_no_overlap;
ALTER TABLE provider_bookings
    ADD CONSTRAINT provider_bookings_no_overlap
    EXCLUDE USING gist (
        provider_id WITH =,
        tstzrange(start_at,end_at,'[)') WITH &&
    ) WHERE (status='booked');

CREATE INDEX IF NOT EXISTS provider_availability_lookup_idx
    ON provider_availability_windows(provider_id,start_at,end_at,window_type);
CREATE INDEX IF NOT EXISTS provider_bookings_order_idx
    ON provider_bookings(order_id,status);

CREATE OR REPLACE FUNCTION reject_provider_calendar_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'provider availability and booking records are immutable';
END $$;
DROP TRIGGER IF EXISTS provider_availability_immutable ON provider_availability_windows;
CREATE TRIGGER provider_availability_immutable BEFORE UPDATE OR DELETE ON provider_availability_windows
FOR EACH ROW EXECUTE FUNCTION reject_provider_calendar_mutation();
DROP TRIGGER IF EXISTS provider_bookings_immutable ON provider_bookings;
CREATE TRIGGER provider_bookings_immutable BEFORE UPDATE OR DELETE ON provider_bookings
FOR EACH ROW EXECUTE FUNCTION reject_provider_calendar_mutation();
