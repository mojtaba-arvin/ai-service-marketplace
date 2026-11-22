# M2–M5 — Catalog, Pricing, Orders and Dispatch Specification

**Feature IDs:** CAT-001–CAT-003, PRICE-001–PRICE-003, ORDER-001–ORDER-004, DISP-001–DISP-003  
**Status:** core vertical slice is implemented; independent acceptance remains pending.

## Ownership and boundaries

- **Catalog (M2):** taxonomy, service eligibility, professional skills/service areas, market visibility and translations. Sources: `catalog.go`, `providers.go`, `catalog_localization.go`.
- **Pricing (M3):** immutable price versions, inspection quotes, customer-approved variations. Sources: `pricing.go`, `quotes.go`, `variations.go`.
- **Orders (M4):** customer-owned order aggregate, schedule and price snapshot, state transitions, cancellation and timeline. Sources: `scheduled_orders.go`, `store.go`, `cancellation.go`, `order_history.go`.
- **Dispatch (M5):** provider availability, deterministic candidate eligibility, bounded offer rounds, acceptance and booking. Sources: `availability.go`, `dispatch_candidates.go`, `dispatch_offers.go`.

## Data ownership

The owning module is responsible for its tables and migrations:
- Catalog: `service_categories`, `service_price_menus` relationships, provider skills/service areas, market visibility and translations.
- Pricing: `service_price_versions`, quote/variation and order-price revision tables.
- Orders: `orders`, cancellation policy/outcome and order history records.
- Dispatch: provider availability/bookings, dispatch rounds/offers/assignments.

The schema is PostgreSQL-first. Other modules must use application interfaces or versioned events rather than importing persistence internals.

## Public HTTP surface

The current web MVP exposes a public catalog; customer order create/history/timeline/cancel; professional profile, qualification, assigned orders, offers and transitions; operator provider/order queues, verification and dispatch. The authoritative route registration is `Handler.serveMVP` in `mvp_http.go`. Contract changes must update the OpenAPI artifact and E2E coverage together.

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

- Catalog: `catalog_test.go`, `catalog_localization_test.go`
- Pricing/quotes: `pricing_test.go`, `quotes_test.go`, `variations_test.go`
- Orders: `scheduled_orders_test.go`, `cancellation_test.go`, `order_history_test.go`
- Dispatch: `availability_test.go`, `dispatch_candidates_test.go`, `dispatch_offers_test.go`
- End-to-end: `apps/web/e2e/marketplace.spec.ts`, `mvp_http_test.go`

Required negatives include cross-customer reads, cross-tenant operator reads, disabled catalog, stale price version, idempotency conflict, invalid state transition, duplicate cancellation, unavailable provider, offer expiry, wrong-provider response, and concurrent acceptance.

## Tenant ownership note

Scheduled orders are being bound to `tenant_id` by migration `023_order_tenant_scope.sql` in the associated hardening PR. Legacy rows without an unambiguous membership remain unassigned and must not be guessed into a tenant. Do not describe tenant isolation as fully accepted until the migration and cross-tenant regression test pass in CI and independent QA.
