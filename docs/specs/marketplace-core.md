# M2–M5 — Catalog/Pricing, Customer Experience, Professional Workspace and Matching Specification

**Feature IDs:** CAT-001–CAT-006, PRICE-001–PRICE-004, ORDER-001–ORDER-007, DISP-001–DISP-006, PRO-001–PRO-003, CLIENT-001–CLIENT-002  
**Status:** repository contains foundational slices; full module scope and independent acceptance remain pending.

## Ownership and boundaries

- **Catalog and pricing (M2):** taxonomy, admin-defined services, versioned dynamic forms, service eligibility, immutable price versions, quotes, variations and market visibility. Sources: `catalog.go`, `providers.go`, `catalog_localization.go`, `pricing.go`, `quotes.go`, `variations.go`, and the `serviceforms` package.
- **Customer experience (M3):** customer-owned order aggregate, schedule/price snapshots, intake, state/timeline, cancellation, completion approval, reviews and reorder. Current sources include `scheduled_orders.go`, `store.go`, `cancellation.go` and `order_history.go`.
- **Professional workspace (M4):** professional profile, skills, availability, offers and fulfillment workflows. Current sources include `providers.go`, `availability.go` and `dispatch_offers.go`.
- **Matching and dispatch (M5):** deterministic candidate eligibility, bounded offer rounds, acceptance, booking and capacity foundations. Sources: `availability.go`, `dispatch_candidates.go`, `dispatch_offers.go`.

## Data ownership

The owning module is responsible for its tables and migrations:
- Catalog: `service_categories`, `service_price_menus` relationships, provider skills/service areas, market visibility and translations.
- Pricing: `service_price_versions`, quote/variation and order-price revision tables.
- Orders: `orders`, cancellation policy/outcome and order history records.
- Dispatch: provider availability/bookings, dispatch rounds/offers/assignments.

The schema is PostgreSQL-first. Other modules must use application interfaces or versioned events rather than importing persistence internals.

## Public HTTP surface

Current browser/API surfaces provide initial catalog, customer order, professional offer and operator workflows. They are not the full customer/professional product surfaces described in Product Scope. The current route registration is `Handler.serveMVP` in `mvp_http.go`. Contract changes must update the OpenAPI artifact and E2E coverage together.

### Incident and safety report intake (TRUST-002)

The initial incident surface exposes `POST /v1/incidents` and reporter-only `GET /v1/incidents/{report_id}`. The caller supplies an order reference, reporter role, category, bounded narrative, optional occurrence time/severity hint, and up to five opaque private evidence references. The API never accepts tenant identity; the Store locks the persisted order, derives its tenant, verifies the customer's ownership or the professional's assignment, and checks the corresponding membership in that tenant.

A submitted report is created with an append-only `submitted` audit event in the same transaction. A reporter can read only their own report; unrelated identities and tenant contexts receive a not-found response. Reporter identity, order binding, narrative, occurrence time and evidence references remain immutable.

The reviewer surface exposes `GET /v1/operator/incidents`, `GET /v1/operator/incidents/{report_id}` and `POST /v1/operator/incidents/{report_id}/transition`. Access requires an active same-tenant operator membership and an explicit `incident:review` grant. Operator membership or the support role alone is insufficient. Assignment targets must be active same-tenant operators with the same explicit grant. Each assignment, severity change or status transition requires a reason and an append-only audit event inserted in the same transaction before the report update; the database trigger rejects unmatched updates. Status transitions are forward-only: `submitted → triaged → investigating → resolved → closed`. Closed reports cannot be changed or reopened. Severity remains a human triage hint and causes no automatic order, payment, provider or emergency action.

Evidence bytes and public URLs are not accepted or stored. Opaque evidence references are visible only to authorized reporters/reviewers and are not download URLs. Private evidence storage/retrieval, access auditing, retention/legal-hold controls and independent acceptance remain separate gates. Every intake response includes the explicit notice that this service is not monitored as an emergency service and that immediate danger requires contacting local emergency services directly. The API contract is in `packages/contracts/openapi.v1.json`.


## Dynamic service questionnaires

The new `internal/serviceforms` package validates versioned, admin-defined questionnaire definitions and submitted answers. It supports a bounded JSON Schema subset: typed objects, strings, integers/numbers, booleans and arrays; required fields; closed objects; enums; string length; numeric bounds; array item bounds; and nested schemas. Unsupported schema keywords are rejected rather than silently ignored.

Resource limits are explicit: 32 KiB schema definitions, 16 KiB answer payloads, nesting depth 8, 64 properties per object, 128 total fields, 100 array items and 32 enum values. Answers are type-checked and unknown object properties are rejected. Definition identity includes service code, version and locale.

The validation boundary is implemented; migration 027 and draft/publish/read APIs provide the market/locale version lifecycle. Migration 028 and scheduled-order creation now validate customer answers against the active published schema and persist the exact form version plus canonical answer JSON in an immutable order snapshot. Customer web questionnaire rendering and structured submission are in PR #183; order-history presentation and independent acceptance remain open and must not be marked complete.


### Service form version lifecycle and publication

Migration 027 adds market/locale-specific form versions keyed by menu, market, locale and version number. Draft versions can be authored only by a platform-tenant operator with catalog-management authority. Publishing is serialized per menu, atomically retires the previous published version, and emits a canonical tenant-scoped `catalog.service_form.published` v1 outbox event. A partial unique index allows only one published version per menu/market/locale. Published and retired schemas are immutable and cannot be deleted; replacement requires a new draft version.

The new authenticated operator endpoints create drafts and publish versions; the public catalog endpoint resolves the published form for a menu/market/locale without exposing operator actor identifiers. The validator from `internal/serviceforms` is used before persistence.

The order API accepts an optional `answers` object. If a published form exists for the service, market and default locale, answers are mandatory and schema-validated before the order is inserted. The exact form version and canonical answers are written in the same transaction as the order; answer snapshots reject UPDATE, DELETE and TRUNCATE. Idempotent replay compares semantic JSON content and returns the original order without switching to a newer published form. Raw answers are deliberately excluded from outbox events. The backend implementation merged in PR #181 and customer web rendering/structured submission merged in PR #183. Order-history presentation and independent acceptance remain open.

## Customer completion approval

The customer can approve a professional-completed order through `POST /v1/orders/{order_id}/completion-approval`. The operation locks the order, verifies customer/tenant ownership and the completed state, then records one immutable approval per order. Repeated requests return the original approval; another customer cannot approve it. The approval is exposed in the customer order timeline. A completed order can also be reordered with `POST /v1/orders/{order_id}/reorder` and a new `scheduled_at`: the service, service-area and prior answer snapshot are reused, while current provider availability, active catalog/price and published questionnaire are revalidated. An optional `answers` object lets the customer update answers when the active form changes. Availability is a preflight rather than a reservation; dispatch and booking recheck it before assignment. This is a workflow acknowledgement only: it does not authorize payment capture, escrow release or settlement, which remain behind the Finance gate.

## Additional customer and professional capabilities

- ORDER-007 owns privacy-scoped live professional location and ETA presentation for assigned orders. Location freshness and uncertainty must be explicit; no stale coordinate is represented as live.
- PRO-001 owns versioned academy content, skill assessments and qualification expiry linked to TRUST verification.
- PRO-002 owns professional CRM, invoices and income reporting; reports reconcile to immutable order/ledger references and cannot mutate financial records.
- PRO-003 owns offline-first professional work capture and idempotent sync. Reconnection always revalidates assignment, expiry, order state and all financial/safety guards.
- DISP-006 ranks only candidates that have passed deterministic eligibility gates and records ranking/fairness inputs for audit.


## Catalog market visibility and localization

Category create, move, enable and disable operations exposed to application handlers must use the actor-authorized `CreateCategoryForActor`, `MoveCategoryForActor` and `SetCategoryEnabledForActor` methods. They require a platform-tenant operator membership and an active `catalog:manage` grant in the same transaction as the write. A transaction-scoped hierarchy lock serializes parent-state validation with category mutations. The older low-level Store helpers are for trusted internal setup only and must not be wired directly to HTTP handlers.

Market visibility is inherited through the full category ancestry. A category is resolvable or listable only when the category and every ancestor are enabled and explicitly visible in the selected market. A visible child beneath a hidden or unconfigured ancestor is not independently discoverable. Locale resolution uses the requested locale only when it is configured for the market; otherwise it falls back to that market's configured default locale. Category visibility and translations are managed through the platform catalog-management authorization boundary. Public route and OpenAPI integration remains owned by CMD-1754.

## Invariants

1. Disabled categories and market-invisible services cannot receive new orders.
2. Price versions are immutable; an order keeps the exact version and amount used.
3. Money is integer minor units with an explicit ISO currency; no floating-point arithmetic.
4. Order creation requires an idempotency key; replay with changed request dimensions conflicts.
5. State changes use a guarded state machine and append an event in the same transaction.
6. A customer can read only their own order; a professional sees only work assigned to their profile; operator access is tenant- and market-scoped.
7. Candidate selection is deterministic and excludes unverified, inactive, unavailable, wrong-area, or wrong-tenant providers.
8. Offer fan-out is bounded and expiring; concurrent acceptance has exactly one winner.
9. Booking and dispatch changes are transactional; outbox events are committed with the aggregate.
10. Cancellation is an audited policy outcome; it does not imply a refund or funds movement.

## Failure and observability

Invalid transitions, changed idempotency payloads, unavailable market/catalog entries, stale/expired offers and conflicting acceptance fail without partial state. Log correlation IDs and aggregate IDs, but do not log bearer tokens, full addresses beyond operational need, or sensitive identity data.

## Verification matrix

- Catalog: `catalog_test.go`, `catalog_localization_test.go`, `internal/serviceforms/serviceforms_test.go`
- Pricing/quotes: `pricing_test.go`, `quotes_test.go`, `variations_test.go`
- Orders: `scheduled_orders_test.go`, `cancellation_test.go`, `order_history_test.go`
- Dispatch: `availability_test.go`, `dispatch_candidates_test.go`, `dispatch_offers_test.go`
- End-to-end: `apps/web/e2e/marketplace.spec.ts`, `mvp_http_test.go`

Required negatives include cross-customer reads, cross-tenant operator reads, disabled catalog, stale price version, idempotency conflict, invalid state transition, duplicate cancellation, unavailable provider, offer expiry, wrong-provider response, and concurrent acceptance.

## Tenant ownership note

Scheduled orders are being bound to `tenant_id` by migration `023_order_tenant_scope.sql` in the associated hardening PR. Legacy rows without an unambiguous membership remain unassigned and must not be guessed into a tenant. Do not describe tenant isolation as fully accepted until the migration and cross-tenant regression test pass in CI and independent QA.
