CREATE TABLE IF NOT EXISTS service_price_menus (
    id text PRIMARY KEY,
    category_id text NOT NULL REFERENCES service_categories(id) ON DELETE RESTRICT,
    service_code text NOT NULL CHECK (service_code ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    active_version_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(category_id, service_code)
);

CREATE TABLE IF NOT EXISTS service_price_versions (
    id text PRIMARY KEY,
    menu_id text NOT NULL REFERENCES service_price_menus(id) ON DELETE RESTRICT,
    version_no integer NOT NULL CHECK (version_no > 0),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(menu_id, version_no)
);

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS price_version_id text;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'service_price_menus_active_version_fk'
          AND conrelid = 'service_price_menus'::regclass
    ) THEN
        ALTER TABLE service_price_menus
            ADD CONSTRAINT service_price_menus_active_version_fk
            FOREIGN KEY (active_version_id) REFERENCES service_price_versions(id)
            DEFERRABLE INITIALLY DEFERRED;
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'orders_price_version_fk'
          AND conrelid = 'orders'::regclass
    ) THEN
        ALTER TABLE orders
            ADD CONSTRAINT orders_price_version_fk
            FOREIGN KEY (price_version_id) REFERENCES service_price_versions(id);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS service_price_versions_menu_idx
    ON service_price_versions(menu_id, version_no DESC);

CREATE OR REPLACE FUNCTION reject_price_version_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'price versions are immutable; create a new version';
END $$;
DROP TRIGGER IF EXISTS service_price_versions_immutable ON service_price_versions;
CREATE TRIGGER service_price_versions_immutable
BEFORE UPDATE OR DELETE ON service_price_versions
FOR EACH ROW EXECUTE FUNCTION reject_price_version_mutation();
