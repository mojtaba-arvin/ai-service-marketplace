CREATE TABLE IF NOT EXISTS market_category_visibility (
    market_code text NOT NULL REFERENCES markets(code) ON DELETE RESTRICT,
    category_id text NOT NULL REFERENCES service_categories(id) ON DELETE RESTRICT,
    visible boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(market_code,category_id)
);

CREATE TABLE IF NOT EXISTS category_translations (
    market_code text NOT NULL,
    category_id text NOT NULL REFERENCES service_categories(id) ON DELETE RESTRICT,
    locale text NOT NULL,
    display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 160),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(market_code,category_id,locale),
    FOREIGN KEY(market_code,locale) REFERENCES market_locales(market_code,locale) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS market_category_visibility_listing_idx
    ON market_category_visibility(market_code,visible,category_id);
