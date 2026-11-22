# ADR 0006: Attribute B2B orders and financial activity safely

- Status: Accepted as a design decision; implementation gated
- Date: 2026-11-22
- Related: [#88](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/88), [#57](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/57), [#10](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/10), [#87](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/87)
- Builds on [ADR 0004](0004-tenant-organization-boundary.md) and [ADR 0005](0005-domain-event-envelope.md)

## Context

The organization/property registry is tenant-scoped, but it is not yet connected to orders or finance:

- `orders` has a nullable `tenant_id` added by migration `023_order_tenant_scope.sql`, but no organization/property attribution. Legacy orders without an unambiguous tenant intentionally remain unassigned.
- `ledger_journals` and `ledger_entries` are immutable double-entry records, but the current journal schema has no tenant or order reference.
- `payment_operations` has provider, operation, amount, currency and idempotency data, but no tenant or order reference. `payment_ledger_links` links a payment operation to one journal, not to an order.
- Therefore, a join from an organization to historical orders or ledger postings cannot currently be made safely. Location similarity, customer identity, payment amount, description text or timestamps are not valid attribution keys.

The tenant boundary and financial invariants must not be weakened to accelerate B2B reporting.

## Decisions

### 1. Order attribution

- An order remains owned by exactly one tenant. Organization and property are optional business attribution within that tenant; neither replaces nor determines `tenant_id`.
- Consumer orders remain unassociated with an organization/property.
- A B2B order must identify both `organization_id` and `property_id`, or neither. An organization-only or property-only association is invalid for the initial B2B order flow.
- The future order schema uses composite references:
  - `(organization_id, tenant_id) -> organizations(id, tenant_id)`
  - `(property_id, tenant_id, organization_id) -> properties(id, tenant_id, organization_id)`
- A database check enforces the all-or-nothing pair. Service authorization additionally requires the organization and property to be active and in the selected tenant at order creation.
- The actor must have an explicit server-side `service_order.create` capability for that organization/property. Existing property delegation currently grants visibility only; it must not be interpreted as permission to place or modify orders. The capability matrix must be specified and tested before enabling B2B order creation.
- Attribution is fixed when the order is created. Moving an order between organizations/properties is not an ordinary update; any future correction requires a dedicated, audited, compensating business operation.
- Existing location fields remain an immutable service-location snapshot. Future B2B orders also snapshot the property display identity needed for historical rendering, so renaming or archiving a property cannot rewrite an old order.

### 2. Financial attribution

- Do not add mutable organization balances or treat an organization as a ledger account.
- Do not infer organization/property attribution from journal descriptions, payment references, customer identity, or approximate matching.
- Before order-linked payment operations are enabled, add explicit tenant/order attribution to the payment-operation boundary, with a composite foreign key to the order. Non-order payment operations may remain unattributed.
- The existing `payment_ledger_links` remains the explicit bridge from a successful payment operation to its immutable ledger journal. The transaction that establishes this link must verify that the payment operation, order and journal currency/amount/tenant context agree.
- Organization/property attribution for a billable journal is resolved through the attributed order. Do not copy mutable organization balances or independently editable organization IDs into ledger accounts.
- Journals that cannot be tied to a validated order/payment source are excluded from B2B customer statements until a separately reviewed source-attribution contract exists.

### 3. Consolidated billing read model

- A consolidated statement is a read model derived from validated immutable ledger postings and their explicit order/payment attribution.
- Projection dimensions include tenant, organization, property, currency and accounting period. Different currencies are never summed together.
- The projection consumes versioned canonical ledger/order events, deduplicates by stable event ID, supports deterministic replay, and includes compensating reversals rather than mutating prior postings.
- Statement lines retain source journal/payment/order identifiers for audit traceability. The read model is rebuildable and is not an authority for posting, payment capture, refunds or settlement.
- Totals are derived from explicitly classified customer-billable accounts and posting semantics; summing every debit or credit in a journal is not a valid invoice total.
- No real-money movement, production billing or invoice issuance is part of this design decision.

## Migration and rollback plan

Implementation must be additive and staged:

1. Complete and independently verify order tenant ownership and cross-tenant access boundaries under #57. Independently verify ledger immutability, balancing, idempotency, reversals and payment reconciliation under #10.
2. Add nullable organization/property attribution to orders with the composite foreign keys, all-or-nothing check, and tenant/org/property indexes. Add constraints as `NOT VALID` where appropriate, validate them after checking existing data, and do not guess/backfill legacy associations.
3. Add optional tenant/order references to order-linked payment operations. Keep the existing payment-to-journal link; enforce source consistency in the transaction and with database constraints where expressible.
4. Add canonical journal-posted/reversed event contracts and producer validation under #87 before building the projection. Events must carry tenant attribution from the authorized source context, not client-supplied values.
5. Build the consolidated statement projection as a replayable, idempotent read model. Validate currency separation, reversals, duplicate delivery, late events and reconciliation against source postings.

Rollback is application-first: disable B2B order attribution and statement reads, then roll back consumers/producers. Keep nullable columns and validated foreign keys in place while data references them. Do not run a destructive down migration that drops populated attribution or changes immutable ledger history. Physical removal is a separate reviewed migration allowed only after proving there are no dependent rows and the feature has been retired.

## Required verification before implementation is enabled

- Valid same-tenant organization/property attribution succeeds.
- Cross-tenant organization, property and mismatched organization/property pairs fail at the database boundary.
- Archived organizations/properties reject new attribution; existing order and financial history remains readable and attributable.
- Revoked memberships lose new B2B order capability immediately; historical order attribution is unaffected.
- Consumer orders remain valid with both B2B references null.
- Legacy orders and journals remain unattributed unless an explicit, audited reconciliation proves their source.
- Payment operation, order and journal amount/currency/source mismatches fail without partial writes.
- Journal immutability, balanced postings, idempotency and compensating reversal invariants remain true.
- Statement projection is deterministic under duplicate/reordered/replayed events, partitions by currency and reconciles to eligible source postings.

## Alternatives rejected

- Make organization the tenant: rejected; it breaks the established security boundary.
- Backfill old orders using customer identity or service-area matching: rejected; attribution would be ambiguous and could leak data.
- Add mutable organization balance fields: rejected; they duplicate and can diverge from immutable ledger postings.
- Join billing directly by free-form journal description or provider reference: rejected; neither is a stable relational contract.
- Enable property delegation managers to create orders implicitly: rejected; visibility and command permissions are separate capabilities.
