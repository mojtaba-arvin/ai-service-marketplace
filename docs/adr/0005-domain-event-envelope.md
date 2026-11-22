# ADR 0005: Versioned Tenant-Scoped Domain Event Envelope

- Status: Accepted
- Date: 2026-11-22

## Context

The transactional outbox already provides durable, at-least-once event delivery, but existing producers use legacy payload shapes. Analytics, integrations and future modules need a stable envelope that carries tenant attribution, aggregate identity, schema version and event identity without reading another module's tables.

## Decision

New domain events use the canonical v1 envelope in `schemas/domain-event.schema.json`:
- stable `id` for idempotent publication and consumption;
- namespaced `type` and positive schema `version`;
- RFC 3339 `occurred_at`;
- mandatory `tenant_id` derived from the authorized aggregate context;
- `aggregate.type` and `aggregate.id`;
- event-specific `data`;
- optional non-sensitive tracing `metadata`, correlation and causation identifiers.

Event data is minimized. Raw phone numbers, credentials, session tokens, private evidence references and unnecessary precise location data are prohibited from analytics-facing events.

Consumers must tolerate at-least-once delivery, deduplicate by event ID, and define behavior for unknown optional fields and unsupported schema versions. Schema changes require compatibility review and replay tests.

Legacy outbox producers are not automatically considered migrated. Adoption is tracked in #87; the schema/example in this repository define the target contract only.

## Consequences

- Event producers need tenant context at the aggregate boundary.
- Consumers can build read models without cross-module operational SQL.
- Event replay and schema evolution become explicit testable behavior.
- The current outbox transport may need a staged compatibility migration; do not rewrite historical payloads in place.

## Alternatives considered

- Keep module-specific unversioned JSON payloads: rejected as the long-term public contract because consumers cannot safely evolve independently.
- Publish entire database rows: rejected due to coupling and privacy exposure.
- Add tenant ID later: rejected because tenant attribution cannot be reliably reconstructed for every historical event.
