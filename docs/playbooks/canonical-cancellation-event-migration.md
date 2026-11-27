# Canonical cancellation-event migration

## Scope

This note covers the existing outbox event types:

- `order.cancelled`
- `provider.booking_cancelled`

Cancellation producers may store the canonical v1 domain-event envelope only when the tenant is derived from the actor already authorized against the persisted order. The cancellation record, order state transition, provider-booking release, and all resulting outbox rows remain in the same PostgreSQL transaction.

The envelope's `data` object retains the existing cancellation business fields. It is not a replacement for the business payload contract.

## Required consumer compatibility boundary

The canonical envelope is an internal persisted representation; it must not silently become the externally visible legacy payload.

The additive adapter tracked by Issue #262 / PR #284 defines the intended boundary:

- HTTP publisher: preserve the historical business object in `payload`; expose the complete validated envelope in optional `domain_event`.
- Customer order history/timeline: preserve the historical business object in `events[].payload`; expose the complete envelope in optional `events[].domain_event`.
- Legacy outbox payloads: forward unchanged and omit `domain_event`; do not infer an envelope from a legacy `version` field.
- Envelope-shaped but malformed or mismatched events: fail closed rather than forwarding an ambiguous representation.

This adapter is a repository-level compatibility mechanism, not proof that every external consumer accepts additive JSON properties. Consumers using closed schemas or strict unknown-field rejection may reject the additional `domain_event` property even though the original `payload` value is preserved.

## Repository-visible compatibility inventory

| Surface | What the repository establishes | External compatibility status |
| --- | --- | --- |
| Outbox HTTP publisher | A generic `OUTBOX_PUBLISH_URL` HTTPS sink is supported; the additive adapter retains legacy `payload` and adds optional `domain_event`. | No deployed URL, sink identity, owner, or sink-owned contract test is recorded. |
| Customer order history/timeline | The first-party API retains `events[].payload` and adds optional `events[].domain_event`. | No inventory of external clients/consumers, deployed schema versions, or owner confirmations is recorded. |
| Internal analytics projector | The repository consumes canonical tenant-scoped events through its own event contract. | Internal behavior does not establish compatibility of the generic HTTP sink or any external timeline client. |

This is an inventory of evidence visible in the repository, not a claim that the listed surfaces are the only deployed consumers. Deployment configuration and consumer ownership must be supplied by the relevant operators/owners before the migration gate can close.

## Rollout / merge gate

Do not enable this migration for existing event types in a production rollout, and do not merge the producer migration as complete, until all of the following are recorded:

1. Inventory each configured external outbox sink and each consumer of the customer timeline; identify an owner and the deployed contract/version for each.
2. Confirm that each consumer accepts the additive `domain_event` property while retaining the legacy `payload` semantics, using consumer-owned contract tests or written owner confirmation.
3. If a consumer rejects additive fields, obtain an explicit versioned endpoint/contract migration or an owner-approved migration window with a dated cutover and rollback plan. Do not silently drop fields or reinterpret old payloads.
4. Record the compatibility evidence and migration window in the tracking issue/PR before merge.
5. Verify the exact PR head against the current main base, including PostgreSQL tests, event contract tests, CI and security checks.

Internal unit, PostgreSQL, publisher, timeline, and CI tests prove repository behavior only. They do not prove external consumer compatibility and must never be cited as that evidence.

## Explicit non-claims

- No external sink or downstream consumer has been identified or confirmed by this document.
- No migration window, consumer approval, production rollout, historical outbox rewrite, or backfill is authorized.
- No B2B/ledger attribution or independent QA result is claimed.
