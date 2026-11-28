# Market configuration contract

The registry is constructed from explicitly supplied market/locale entries and then treated as immutable.

Each entry requires an uppercase market identifier of 2–8 letters (matching the authoritative database constraint), a language/region locale, an uppercase three-letter currency code, a valid IANA time-zone name, and an explicit policy version. The registry validates the currency code's shape; approved market configuration remains responsible for supplying a valid ISO currency.

Resolution is exact. There is no default market, locale fallback, currency inference, or policy-version fallback. Unknown and malformed combinations fail closed.

The application must supply an approved registry. This package does not infer legal market availability or make payment, KYC, tax, or labor-policy decisions.


## Locale direction and adapter capabilities

`Config.Direction()` derives LTR/RTL from the configured locale language subtag. It does not infer direction from country or fall back to another locale.

Build a `Matrix` from explicit `AdapterConfig` rows after constructing the market registry. Every row is tied to the exact `PolicyVersion` of its market/locale configuration. `Require` checks the exact market, locale, adapter and capability tuple; unsupported adapters and capabilities return distinct typed errors. There is no cross-market or cross-locale fallback.

The matrix is immutable after construction and contains no provider credentials or network clients. Its rows must come from the deployment's approved, version-controlled market/provider configuration. This package deliberately does not invent market availability, legal policy, or provider support.
