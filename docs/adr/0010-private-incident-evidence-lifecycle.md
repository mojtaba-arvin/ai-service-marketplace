# ADR 0010: Private incident evidence retention and deletion lifecycle

- **Status:** Proposed
- **Date:** 2026-11-26
- **Owners:** Marketplace platform maintainers
- **Related:** ADR 0008 (incident reviewer workflow), ADR 0009 (private incident evidence), #217, #11

## Context

Migration 035 and PR #219 provide tenant-bound evidence metadata, encrypted private storage, API-mediated upload/retrieval, ClamAV fail-closed scanning and append-only access audit. The current retention boundary denies retrieval after `retention_expires_at`, but it does not delete the encrypted object. `legal_hold` is metadata only and has no independently permissioned management workflow.

Deletion is destructive and may be governed by an adopter's legal, contractual and operational policy. The platform must not invent a universal retention duration, infer a legal hold, or silently remove evidence when no retention policy is configured.

## Decision

### 1. Separate evidence lifecycle authority

Introduce the operator action `incident:evidence:manage`. It is independent from `incident:review`, `operator:permissions:manage` and Support membership.

- Only an active same-tenant operator membership with an explicit `incident:evidence:manage` grant may change retention or legal-hold state.
- Reviewer access never implies lifecycle-management authority.
- Support membership alone never authorizes evidence management.
- Grant and revoke continue to use the existing transaction-bound operator permission model; no grandfathering or self-grant is introduced.
- Every management request requires a bounded, non-empty reason and writes an append-only audit event in the same transaction as the metadata change.

### 2. Explicit per-evidence retention; no platform default

Keep retention adopter-configured. The platform does not assign a default expiry and does not start automatic deletion when `retention_expires_at` is null.

An authorized evidence manager may set, change or clear an evidence expiry. The API accepts an explicit timestamp (or explicit null to clear it), validates and normalizes it to UTC, and records before/after values plus the actor and reason. An expiry in the past is valid as an explicit immediate-expiry instruction, but bytes are deleted only by the lifecycle worker.

A legal hold is an independent boolean control. Setting and clearing it both require an explicit reason and audit. A held object is never eligible for expiry deletion, even when its configured expiry is in the past.

### 3. Deletion is a retryable state machine

Retain the evidence metadata row as a tombstone; do not delete it as part of byte removal.

States:

- `active`: object may be retrieved subject to expiry and authorization checks.
- `deletion_pending`: an eligible expiry has been claimed and the worker may retry object deletion. Retrieval is denied.
- `deleted`: storage deletion has been confirmed (or the object was already absent). Retrieval remains denied permanently.

The worker processes only rows where an explicit expiry is due and `legal_hold=false`, plus rows already in `deletion_pending`. It uses bounded batches, row locking with `FOR UPDATE SKIP LOCKED`, and commits the pending state and an audit event before making the storage call.

Storage I/O occurs outside the database transaction. The worker then records one of:

- **Success:** mark the row `deleted`, set `deleted_at`, and append a success event.
- **Failure:** leave the row `deletion_pending`, increment a bounded attempt counter, record a sanitized failure category and append a failure event. A later worker run retries it.

A missing storage object is treated as successful deletion. Storage adapters must implement idempotent delete semantics. Metadata is never removed before deletion is confirmed.

A crash after claiming but before deletion is recovered by retrying `deletion_pending`. A crash after storage deletion but before database finalization is also safe: the next delete attempt observes an absent object and finalizes the tombstone.

### 4. Legal-hold race and transition rules

Lifecycle management and deletion claim serialize on the same evidence row.

- A manager may set or clear legal hold only while the row is `active`.
- Once a worker commits `deletion_pending`, a new legal hold or retention edit returns a conflict; deletion has already been claimed.
- A worker must check `legal_hold=false` and due expiry while holding the row lock before claiming.
- Clearing a legal hold does not delete immediately; a later worker run may claim the now-eligible expired object.
- The worker never overrides or clears legal hold.

This gives the adopter a deterministic point at which an expiry claim becomes irreversible. It avoids pretending that a hold can recall bytes after a storage deletion has begun.

### 5. Audit model

Extend the append-only evidence access-event vocabulary for lifecycle operations, including retention updates, legal-hold set/clear, expiry claim, deletion success and deletion failure. Keep actor identity, tenant/report/evidence IDs, action, outcome, bounded reason category and timestamp. Never store evidence bytes, client filenames, object paths, storage URLs, encryption keys, scanner output or raw provider errors in audit rows.

The lifecycle audit event and corresponding metadata state change must commit in the same transaction. Worker deletion outcomes must be auditable and retryable without rewriting prior events.

### 6. Backup and restore boundary

Deleting the live object does not delete copies in adopter-managed backups. A deployment is not retention-compliant merely because the live object was removed.

- The adopter must configure backup retention to satisfy its approved policy and document the relationship to evidence expiry.
- Backup/restore runbooks must restore the database, evidence volume and keyring as one recovery set.
- After restore, the worker must replay retained `deletion_pending` rows and deletion tombstones before evidence retrieval is enabled.
- Do not automatically purge historical backups or invent a universal backup lifetime.
- Independent restore rehearsal must prove that a deleted object does not become retrievable again after restoration and that legal-held evidence remains available.

### 7. Key rotation and re-encryption

Retain historical key IDs until all objects encrypted under them have been re-encrypted or explicitly retained for restore. Rotation must be an explicit, observable operation that verifies each decrypted object before writing the replacement envelope with the active key. It must be restartable and must not destroy the previous recoverable object/key before verification and durable replacement succeed.

The restore procedure must prove that the active and required historical keys are available. A successful key lookup unit test is not a restore rehearsal.

## API and worker contract

The implementation should expose a tenant-scoped operator lifecycle endpoint under:

`/v1/operator/incidents/{report_id}/evidence/{evidence_id}/lifecycle`

The endpoint accepts only explicit lifecycle fields, a reason, and the authenticated operator context. Tenant identity is derived from the authenticated actor and verified against both report and evidence rows. Unknown fields, invalid timestamps, mismatched report/evidence pairs, inactive or ungranted operators, and attempts to mutate a non-active object fail closed.

A separately deployable retention worker uses the existing private evidence adapter and database. It must have no reviewer or operator session and must not expose an HTTP endpoint that can delete arbitrary object IDs. Worker configuration must be explicit; an absent retention policy means no expiry candidates, not a default schedule.

## Consequences

- The existing evidence implementation remains safe for adopters without a retention policy: it does not auto-delete.
- Evidence managers have narrower, auditable authority than reviewers.
- Expiry becomes eventual rather than instantaneous; the UI/API must show that an object is pending deletion or deleted without exposing storage internals.
- Database tombstones and audit events remain available for accountability after byte deletion.
- Backup retention and key lifecycle remain adopter operational responsibilities and must be documented and rehearsed.
- TRUST-002 remains incomplete until the permission, management API, worker, retry behavior, backup/restore procedure, regression tests and independent acceptance are delivered.

## Non-goals

- A universal retention duration or legal policy.
- Automatic legal-hold inference, case adjudication or emergency response.
- Deletion of adopter backups by the application.
- Public or signed download URLs.
- AI decisions about evidence retention, deletion or legal hold.
