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

Tracking: [#87](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/87). Architecture decision: [ADR 0005](../adr/0005-domain-event-envelope.md).

The canonical event envelope is defined in `schemas/domain-event.schema.json`. The outbox package now exposes `AppendDomainEvent` to validate and persist a canonical envelope in the aggregate transaction, including required tenant attribution and a recursive prohibited-field-name guard. `Append` remains for legacy compatibility; existing producers are not yet fully migrated and analytics consumers must reject or quarantine legacy payloads rather than silently reinterpret them. Analytics consumers must receive minimized, pseudonymized event data and must not query operational tables as an implicit cross-module API.

Metric definitions must specify event source, denominator, time window, timezone, deduplication and late-arrival behavior. Funnel and supply metrics are not accepted until replay, duplicate delivery and schema-version compatibility are tested.

## B2B (M13)

Tracking: [#88](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/88). Architecture decision: [ADR 0004](../adr/0004-tenant-organization-boundary.md).

### Implemented on `main`

Migration `024_organizations_properties.sql` and the organization API provide a tenant-scoped B2B foundation:

- Create an organization for an authenticated customer/professional identity in the selected tenant; create the initial owner membership in the same transaction.
- Read organizations and active properties only after checking current organization membership. Owners/admins/managers see all active properties; members/viewers see only explicitly delegated properties.
- Create properties only for active owner/admin/manager memberships.
- Invite identities already registered as customer/professional in the same tenant; invitations are pending memberships and are surfaced through `GET /v1/organization-invitations` (maximum 100, newest first) for the authenticated identity and selected tenant.
- The invitee alone can accept its pending invitation. Membership role changes enforce owner/admin/manager policy. Revocation atomically revokes the organization membership and that identity's active property delegations.
- Organization writes and membership lifecycle mutations persist append-only audit records and canonical tenant-scoped outbox events in the same database transaction.
- Organization owners/admins can update organization names/slugs; only owners can archive an organization. Archiving is soft and terminal in this API: it archives child properties, revokes all organization memberships and property delegations, and retains historical rows.
- Owners/admins/managers can update active property metadata; owners/admins/managers can archive properties. Property archive is soft, revokes active delegations, and preserves the property row for downstream order/ledger history.
- Property delegation APIs list, grant, change and revoke property visibility. Owners/admins can grant manager/viewer and change either role; organization managers can grant/revoke viewer only. Only active organization members with member/viewer roles can receive delegations. Property delegation roles currently grant visibility only and do not confer organization-management or property-write authority.
- The API contract is maintained in `packages/contracts/openapi.v1.json`; tenant selection through `X-Tenant-ID` is a context selector, never proof of authorization.

### Authorization invariants

- Tenant is the security/data-isolation boundary; organization is only a business grouping inside that tenant.
- Every operation resolves the authenticated identity against the selected tenant and current organization membership in the database. Token claims and client-supplied tenant IDs are not sufficient authorization.
- Authorization is deny-by-default. Owners may manage all organization roles; admins cannot grant/manage owners; managers may manage only members/viewers and assign only member/viewer. Other roles cannot manage membership.
- The last active owner cannot be demoted or revoked. Membership mutation transactions serialize on the organization row to prevent concurrent last-owner races.
- Property delegation management is limited to owner/admin/manager actors; manager actors may grant/revoke viewer access only. Delegation targets must be active organization members, and property delegation roles currently control visibility only. Changing an organization role across the eligible member/viewer boundary revokes the identity's active property delegations in the same transaction; member<->viewer changes preserve them. Demotion does not reactivate revoked grants; an explicit regrant is required.
- Archiving is non-destructive: archived organizations and properties remain stored; organization archive revokes all memberships and archives child properties, while property archive revokes only that property's delegations. No order or ledger rows are deleted.
- Cross-tenant organization, member, property, delegation and audit-actor references are rejected by composite database constraints.


### Order and financial relationship — designed, not yet implemented

The organization/property registry is not yet linked to order or finance records. The current order schema has tenant ownership but no organization/property IDs; the current payment operations and ledger journals do not carry a reliable order/tenant attribution key. It is unsafe to infer that relationship from customer identity, service-area text, amount, timestamps or journal descriptions.

[ADR 0006](../adr/0006-b2b-order-finance-attribution.md) defines the staged contract:

- A consumer order has neither B2B reference; a B2B order has both organization and property references, constrained to the same tenant and organization.
- The order's tenant remains the security boundary. The actor must hold a separately authorized `service_order.create` capability; property visibility delegation alone is insufficient.
- Attribution is fixed at order creation and the service-location display data is snapshotted. Property rename/archive never rewrites historical orders.
- Order-linked payment operations must carry validated tenant/order references. Existing `payment_ledger_links` then provides the explicit payment-to-journal bridge. Unattributed journals are excluded from organization statements.
- Consolidated statements are replayable read models over eligible immutable postings, partitioned by tenant, organization, property, currency and period; reversals are included and currencies are never combined. No mutable organization balance is introduced.

No association columns, historical backfill, financial read model or billing operation is included in the current implementation. Implementation is gated on independent order tenant-isolation QA (#57), finance invariant QA (#10), and canonical event producer/consumer readiness (#87). Legacy rows remain unassigned unless explicitly reconciled with auditable evidence.

### Explicit limits

Invitation delivery is in-app only: no email/SMS, no invitation to identities that do not yet exist in the tenant, and no invitation expiry/re-send workflow. Pending-invitation pagination beyond the bounded first 100, order/ledger aggregation and consolidated billing remain deferred. No payment capture, money movement or production billing is introduced.

Merged implementation and passing CI are not independent QA. The feature inventory remains `passes: false` until independent negative-path and acceptance evidence is recorded.

## AI intake and evaluation (M14)

Tracking: [#89](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/89).

AI intake is a non-authoritative suggestion boundary. It receives redacted text, market/locale and allowed catalog categories; validates strict structured output; records model/version/input hash; and must not create orders or decide money, safety, identity, reputation or disputes. Implementation: `internal/aiintake`, `internal/aieval`, `internal/aiintake`.

Golden datasets are versioned and deterministic. Critical-case failures fail evaluation even if aggregate scores pass. No real customer PII or secrets belong in datasets. Model calls are not required for offline CI.


### Professional Copilot boundary

The new `internal/procopilot` package defines an evidence-grounded, advisory boundary for troubleshooting summaries, safety warnings, execution steps, checklists and quote-scope drafts. Retrieval is scoped by market, locale and service code. User text and retrieved evidence are redacted before model invocation; output citations must refer to evidence returned by the retrieval adapter. Strict structured output rejects unknown fields (including payable amounts), invented citations and unbounded content.

Every suggestion is marked `requires_professional_review=true`. The package has no persistence or command execution capability and cannot approve a quote, mutate an order, set a payable amount or decide safety. The versioned golden set and deterministic evaluator are in `internal/procopilot/golden/v1`; offline tests cover redaction, citation provenance, unknown money fields and critical-case regressions.

This is a reusable service boundary, not yet a production API/UI integration or evidence of independent AI acceptance. Provider wiring, real knowledge-source integration, operator/professional UX and independent evaluation remain open.

## Cross-cutting verification

- Outbox: `internal/outbox/*_test.go`, `outbox_integration_test.go`.
- Adapters: `internal/adapters/runner_test.go`.
- Market config: `internal/marketconfig/config_test.go`.
- AI: `internal/aieval/evaluation_test.go`, `internal/aiintake/service_test.go`, `internal/procopilot/service_test.go`, `internal/procopilot/evaluation_test.go`.

## Explicitly deferred

Do not mark GROW, AN, B2B or unimplemented ADMIN/MSG/PLAT features complete based on these foundations. Each requires a focused spec, data contract, authorization model, migration plan, offline tests and independent acceptance evidence.
