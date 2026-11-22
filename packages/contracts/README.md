# API and Event Contracts

This directory is the versioned public contract surface for clients, integrations and asynchronous consumers.

## Current artifacts

- `../schemas/domain-event.schema.json` defines the target v1 canonical event envelope.
- `../schemas/examples/order-created.v1.json` is a PII-minimized example conforming to that envelope.

The canonical event schema is a contract target; current outbox producers still emit legacy payload shapes. Do not claim full adoption until producers, publisher metadata and consumers are migrated with compatibility tests.

## HTTP API

The current MVP routes are implemented in `apps/api/internal/marketplace/http.go` and `mvp_http.go`. A complete generated OpenAPI document is not yet published. Until it is, changes to those routes must be reflected in the module specifications, handler tests and browser E2E. Do not hand-author undocumented response fields or assume internal store structs are public DTOs.

## Compatibility rules

- Breaking HTTP/event changes require an explicit versioning and migration plan.
- Events are additive within a major schema version; consumers must ignore unknown optional fields.
- Event IDs are stable idempotency keys; delivery is at-least-once.
- Tenant identity is mandatory for new tenant-scoped domain events.
- Generated artifacts must be reproducible and committed only when their source contract changes.
- Keep contract tests offline and independent of production credentials.
