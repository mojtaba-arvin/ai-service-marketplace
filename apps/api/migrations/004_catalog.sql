CREATE TABLE IF NOT EXISTS service_categories (
    id text PRIMARY KEY,
    parent_id text REFERENCES service_categories(id) ON DELETE RESTRICT,
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    display_name text NOT NULL CHECK (length(btrim(display_name)) > 0),
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (parent_id, slug),
    CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE INDEX IF NOT EXISTS service_categories_parent_idx ON service_categories(parent_id);

CREATE OR REPLACE FUNCTION prevent_service_category_cycle() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    has_cycle boolean;
BEGIN
    IF NEW.parent_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.parent_id = NEW.id THEN
        RAISE EXCEPTION 'category cannot be its own parent';
    END IF;
    WITH RECURSIVE ancestors AS (
        SELECT id, parent_id FROM service_categories WHERE id = NEW.parent_id
        UNION ALL
        SELECT c.id, c.parent_id
        FROM service_categories c
        JOIN ancestors a ON c.id = a.parent_id
    )
    SELECT EXISTS(SELECT 1 FROM ancestors WHERE id = NEW.id) INTO has_cycle;
    IF has_cycle THEN
        RAISE EXCEPTION 'category hierarchy cannot contain a cycle';
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS service_category_cycle_guard ON service_categories;
CREATE TRIGGER service_category_cycle_guard
BEFORE INSERT OR UPDATE OF parent_id ON service_categories
FOR EACH ROW EXECUTE FUNCTION prevent_service_category_cycle();
