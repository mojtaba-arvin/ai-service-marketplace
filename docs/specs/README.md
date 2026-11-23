# Module Specifications

These specifications define module ownership, public boundaries, data ownership, invariants, authorization, events, failure behavior, observability and executable acceptance criteria. They are engineering contracts, not evidence that every listed capability is implemented.

## Specifications

| Domain | Modules / feature IDs | Specification |
|---|---|---|
| Identity and access | M1 / IDENT-001–009 | [identity-and-access.md](identity-and-access.md) |
| Marketplace core | M2–M5 / CAT, PRICE, ORDER, DISP, PRO, CLIENT | [marketplace-core.md](marketplace-core.md) |
| Trust, safety and support | M7–M9 / TRUST, SUP, ADMIN | [trust-and-safety.md](trust-and-safety.md) |
| Finance | M6 / FIN-001–008 | [finance.md](finance.md) |
| Platform, messaging, admin, growth, analytics, B2B and AI | M8–M14 / MSG, ADMIN, GROW, AN, B2B, PLAT, AI | [platform-analytics-b2b-ai.md](platform-analytics-b2b-ai.md) |

## Sources of truth

- `feature_list.json` defines feature IDs, dependencies and acceptance status.
- This directory defines module ownership and behavioral contracts.
- `packages/contracts` contains versioned external API/event contracts.
- Tests are executable evidence; documentation does not replace them.
- `docs/progress.md` records integrated progress and known limitations.

## Change discipline

A behavior change must update the owning specification, API/event contract, migration and tests in the same change where applicable. A specification-only PR must label implementation as planned or existing-but-unverified; it must not claim the feature is complete.

Every acceptance criterion should map to at least one test or explicitly state why it cannot be automated. Security, financial and tenant-boundary criteria require negative-path tests. Only an independent verifier may change `passes` or `verified_by` in the feature inventory.
