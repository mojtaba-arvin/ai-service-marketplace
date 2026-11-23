CREATE TABLE IF NOT EXISTS service_form_versions (
    id text PRIMARY KEY,
    menu_id text NOT NULL REFERENCES service_price_menus(id) ON DELETE RESTRICT,
    market_code text NOT NULL,
    locale text NOT NULL,
    version_no integer NOT NULL CHECK (version_no > 0),
    schema jsonb NOT NULL CHECK (jsonb_typeof(schema) = 'object'),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','retired')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_by text,
    published_at timestamptz,
    retired_by text,
    retired_at timestamptz,
    UNIQUE(menu_id,market_code,locale,version_no),
    FOREIGN KEY (market_code,locale) REFERENCES market_locales(market_code,locale) ON DELETE RESTRICT,
    CHECK ((status='draft' AND published_at IS NULL AND published_by IS NULL AND retired_at IS NULL AND retired_by IS NULL)
        OR (status='published' AND published_at IS NOT NULL AND published_by IS NOT NULL AND retired_at IS NULL AND retired_by IS NULL)
        OR (status='retired' AND retired_at IS NOT NULL AND retired_by IS NOT NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS service_form_one_published_per_market_locale
    ON service_form_versions(menu_id,market_code,locale) WHERE status='published';
CREATE INDEX IF NOT EXISTS service_form_versions_lookup_idx
    ON service_form_versions(menu_id,market_code,locale,version_no DESC);

CREATE OR REPLACE FUNCTION guard_service_form_version_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'service form versions are immutable records; retire instead';
    END IF;
    IF OLD.menu_id<>NEW.menu_id OR OLD.market_code<>NEW.market_code OR OLD.locale<>NEW.locale
       OR OLD.version_no<>NEW.version_no OR OLD.created_by<>NEW.created_by OR OLD.created_at<>NEW.created_at THEN
        RAISE EXCEPTION 'service form version identity is immutable';
    END IF;
    IF OLD.status IN ('published','retired') AND OLD.schema IS DISTINCT FROM NEW.schema THEN
        RAISE EXCEPTION 'published service form schema is immutable; create a new version';
    END IF;
    IF OLD.published_at IS NOT NULL AND (NEW.published_at IS DISTINCT FROM OLD.published_at OR NEW.published_by IS DISTINCT FROM OLD.published_by) THEN
        RAISE EXCEPTION 'service form publication metadata is immutable';
    END IF;
    IF OLD.retired_at IS NOT NULL AND (NEW.retired_at IS DISTINCT FROM OLD.retired_at OR NEW.retired_by IS DISTINCT FROM OLD.retired_by) THEN
        RAISE EXCEPTION 'service form retirement metadata is immutable';
    END IF;
    IF OLD.status='retired' AND NEW.status<>'retired' THEN
        RAISE EXCEPTION 'retired service form versions cannot be reactivated';
    END IF;
    IF OLD.status='published' AND NEW.status NOT IN ('published','retired') THEN
        RAISE EXCEPTION 'published service form can only be retired';
    END IF;
    IF OLD.status='draft' AND NEW.status NOT IN ('draft','published') THEN
        RAISE EXCEPTION 'invalid service form lifecycle transition';
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS service_form_versions_guard ON service_form_versions;
CREATE TRIGGER service_form_versions_guard
BEFORE UPDATE OR DELETE ON service_form_versions
FOR EACH ROW EXECUTE FUNCTION guard_service_form_version_mutation();

CREATE OR REPLACE FUNCTION reject_service_form_versions_truncate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'service form version history cannot be truncated';
END $$;
DROP TRIGGER IF EXISTS service_form_versions_no_truncate ON service_form_versions;
CREATE TRIGGER service_form_versions_no_truncate
BEFORE TRUNCATE ON service_form_versions
FOR EACH STATEMENT EXECUTE FUNCTION reject_service_form_versions_truncate();
