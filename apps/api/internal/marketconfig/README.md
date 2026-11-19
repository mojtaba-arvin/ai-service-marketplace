# Market configuration contract

The registry is constructed from explicitly supplied market/locale entries and then treated as immutable.

Each entry requires an uppercase two-letter market identifier, language/region locale, uppercase three-letter currency code, valid IANA time-zone name, and explicit policy version.

Resolution is exact. There is no default market, locale fallback, currency inference, or policy-version fallback. Unknown and malformed combinations fail closed.

The application must supply an approved registry. This package does not infer legal market availability or make payment, KYC, tax, or labor-policy decisions.
