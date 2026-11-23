# M2–M5 — Catalog/Pricing, Customer Experience, Professional Workspace and Matching Specification

**Feature IDs:** CAT-001–CAT-004, PRICE-001–PRICE-004, ORDER-001–ORDER-006, DISP-001–DISP-005, CLIENT-001–CLIENT-002  
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


## Dynamic service questionnaires

The new `internal/serviceforms` package validates versioned, admin-defined questionnaire definitions and submitted answers. It supports a bounded JSON Schema subset: typed objects, strings, integers/numbers, booleans and arrays; required fields; closed objects; enums; string length; numeric bounds; array item bounds; and nested schemas. Unsupported schema keywords are rejected rather than silently ignored.

Resource limits are explicit: 32 KiB schema definitions, 16 KiB answer payloads, nesting depth 8, 64 properties per object, 128 total fields, 100 array items and 32 enum values. Answers are type-checked and unknown object properties are rejected. Definition identity includes service code, version and locale.

This is a pure validation boundary only. Admin authorization, persistence/publication lifecycle, API endpoints, immutable order schema/answer snapshots, generated client forms and E2E coverage remain unimplemented. The feature must not be marked complete until those integration layers and independent acceptance exist.

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
