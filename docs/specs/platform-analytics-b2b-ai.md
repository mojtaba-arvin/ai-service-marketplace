# M8, M10–M14 — Platform, Messaging, Admin, Analytics, B2B and AI Specification

**Feature IDs:** MSG-001–MSG-003, ADMIN-001–ADMIN-003, GROW-001–GROW-002, AN-001–AN-002, B2B-001–B2B-003, PLAT-001–PLAT-004  
**Status:** adapter, outbox, market-config and AI intake/evaluation foundations exist. Growth, analytics, B2B and several admin capabilities remain backlog.

## Platform and adapters (M14)

Market configuration is explicit and immutable after registry construction. Country/market code, locale, currency, IANA timezone and policy version are required; resolution is exact and fails closed. Implementation: `internal/marketconfig`, `internal/adapters`.

External effects are behind provider-neutral interfaces. Each adapter needs a deterministic fake, contract tests, bounded timeout/retry, idempotency and unknown-outcome handling. No live credentials are required in CI.

## Messaging (M8)

The transactional outbox is written in the same PostgreSQL transaction as the aggregate. Publishers claim bounded leased batches; delivery is at-least-once; consumers deduplicate by stable event ID and commit the processed marker with business effects. Implementation: `internal/outbox`, `internal/marketplace/outbox.go`, migrations `001` and `019`.

Notification preferences and provider delivery adapters remain distinct backlog work. Do not equate a persisted outbox event with successful user notification.

## Admin (M10)

The API is the authorization boundary. Operator/support roles are tenant-scoped and sensitive actions require audit evidence. The admin UI must never rely on hidden controls as authorization. Market configuration changes require explicit permission, validation, audit and fail-closed rollback.

## Analytics (M12)

The canonical event envelope is defined in `schemas/domain-event.schema.json` as a target contract. Existing outbox payloads are not yet fully migrated to this envelope. Analytics consumers must receive minimized, pseudonymized event data and must not query operational tables as an implicit cross-module API.

Metric definitions must specify event source, denominator, time window, timezone, deduplication and late-arrival behavior. Funnel and supply metrics are not accepted until replay, duplicate delivery and schema-version compatibility are tested.

## B2B (M13)

Organization membership, property registry and consolidated statements are backlog capabilities, not current MVP functionality. The future model must distinguish tenant (security/data boundary) from organization (business grouping). An organization may not bypass identity membership or tenant isolation. Property/location records need explicit ownership, access delegation, archival and audit rules. Consolidated billing is a read model over immutable financial postings, not a mutable balance field.

## AI intake and evaluation (M14)

AI intake is a non-authoritative suggestion boundary. It receives redacted text, market/locale and allowed catalog categories; validates strict structured output; records model/version/input hash; and must not create orders or decide money, safety, identity, reputation or disputes. Implementation: `internal/aiintake`, `internal/aieval`, `internal/aiintake`.

Golden datasets are versioned and deterministic. Critical-case failures fail evaluation even if aggregate scores pass. No real customer PII or secrets belong in datasets. Model calls are not required for offline CI.

## Cross-cutting verification

- Outbox: `internal/outbox/*_test.go`, `outbox_integration_test.go`.
- Adapters: `internal/adapters/runner_test.go`.
- Market config: `internal/marketconfig/config_test.go`.
- AI: `internal/aieval/evaluation_test.go`, `internal/aiintake/service_test.go`.

## Explicitly deferred

Do not mark GROW, AN, B2B or unimplemented ADMIN/MSG/PLAT features complete based on these foundations. Each requires a focused spec, data contract, authorization model, migration plan, offline tests and independent acceptance evidence.
