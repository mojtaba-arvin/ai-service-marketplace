CREATE TABLE IF NOT EXISTS markets (
    code text PRIMARY KEY CHECK (code ~ '^[A-Z]{2,8}$'),
    default_locale text NOT NULL CHECK (default_locale ~ '^[a-z]{2,3}(-[A-Z]{2})?$'),
    currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    timezone text NOT NULL,
    policy_version text NOT NULL CHECK (length(btrim(policy_version)) BETWEEN 1 AND 80),
    enabled boolean NOT NULL DEFAULT false,
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS market_locales (
    market_code text NOT NULL REFERENCES markets(code) ON DELETE CASCADE,
    locale text NOT NULL CHECK (locale ~ '^[a-z]{2,3}(-[A-Z]{2})?$'),
    is_default boolean NOT NULL DEFAULT false,
    PRIMARY KEY(market_code,locale)
);
CREATE UNIQUE INDEX IF NOT EXISTS market_locales_one_default_idx
    ON market_locales(market_code) WHERE is_default=true;
