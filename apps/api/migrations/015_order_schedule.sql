ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS scheduled_at timestamptz,
    ADD COLUMN IF NOT EXISTS country_code char(2),
    ADD COLUMN IF NOT EXISTS region_code text,
    ADD COLUMN IF NOT EXISTS city_code text,
    ADD COLUMN IF NOT EXISTS area_code text;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='orders_service_area_complete') THEN
        ALTER TABLE orders ADD CONSTRAINT orders_service_area_complete CHECK (
            (scheduled_at IS NULL AND country_code IS NULL AND region_code IS NULL AND city_code IS NULL AND area_code IS NULL)
            OR
            (scheduled_at IS NOT NULL AND length(country_code)=2 AND country_code ~ '^[A-Z][A-Z]'
                AND length(btrim(region_code))>0 AND length(btrim(city_code))>0 AND length(btrim(area_code))>0)
        );
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS orders_requested_schedule_idx
    ON orders(scheduled_at) WHERE status='requested' AND scheduled_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS orders_service_area_idx
    ON orders(country_code,region_code,city_code,area_code) WHERE scheduled_at IS NOT NULL;
