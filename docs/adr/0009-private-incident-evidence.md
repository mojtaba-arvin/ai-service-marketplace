# ADR 0009 — Private incident evidence storage and retrieval

- **Status:** Accepted
- **Date:** 2026-11-26
- **Scope:** M7 / TRUST-002 / Issue #217

## Context

Incident intake (#211) and the dedicated same-tenant reviewer workflow (#215) persist opaque evidence identifiers, but do not store or retrieve evidence bytes. An opaque identifier is not a storage security boundary. The product is self-hostable and must not require a single cloud vendor. The current deployment profile is a single-host Compose topology; no distributed object store, malware scanner, retention duration, legal policy or production key-management service has been selected by adopters.

## Decisions

1. **Domain/storage separation.** The marketplace domain depends on a narrow private-evidence interface, not a cloud SDK or public URL. Storage identifiers are random opaque IDs; provider object keys and filesystem paths never appear in API responses, event payloads, logs or audit records.
2. **API-mediated access.** Upload and download pass through the API. Do not issue public URLs, presigned URLs or long-lived bearer links. Every request is authorized against the persisted incident/report and tenant at the time of access.
3. **Authorization.** Upload is limited to the original reporter while the report is eligible for evidence submission. Retrieval is limited to that reporter and an active same-tenant operator with the explicit `incident:review` grant. Support role or operator membership alone is insufficient. Closed reports do not permit evidence mutation; any later exception requires a new reviewed policy.
4. **Private-by-default storage.** Evidence is stored outside the web root and inaccessible through static serving. The initial self-hosted adapter may use a private filesystem root; the interface must allow a separately configured object-store adapter without changing domain behavior. Production startup fails closed when storage configuration or required key material is absent.
5. **Encryption.** Encrypt evidence before persistence using authenticated encryption and a versioned key identifier. Keep keys outside the database and object store; support key rotation without exposing old keys. Self-hosters must document encrypted backups and recovery. Do not claim encryption-at-rest solely because the host volume is encrypted.
6. **Content validation.** Enforce adopter-configured maximum bytes, allowed media types and per-report item count. Determine media type from bytes, not filename or client headers; generate storage names; reject path/control characters and unsupported formats. Production upload requires a configured malware/content scanner. Scanner timeout, rejection or unavailability fails closed; quarantine bytes must not be retrievable.
7. **Access audit.** Record successful and denied upload/retrieval attempts with tenant, report/evidence opaque IDs, actor, action, outcome category and timestamp in append-only storage. Never persist evidence bytes, raw filenames, scanner response bodies, credentials, signed URLs or raw request payloads in audit/logs. Audit writes and state changes must be transactionally consistent where possible; an audit failure must prevent evidence disclosure.
8. **Retention and legal hold.** No universal duration is defined. Retention is adopter policy and must be explicit. Do not automatically delete evidence when no valid retention policy is configured. A legal hold or open investigation blocks deletion. Deletion must be auditable, idempotent and coordinated with backups according to the adopter's documented recovery policy.
9. **Failure behavior.** Missing object, key mismatch, decryption/authentication failure, scanner outage, storage outage, unauthorized actor and cross-tenant access fail closed. Do not fall back to public storage, unencrypted persistence or unscanned upload.
10. **No emergency-response implication.** Evidence storage does not provide monitoring, dispatch or guaranteed response. The existing emergency-service notice remains mandatory.

## Consequences

- The first implementation is intentionally API-mediated and provider-neutral, suitable for the current single-host deployment without introducing a broker, cache or cloud SDK.
- Deployment configuration must expose only the storage root, encryption key reference, upload limits, allowed media types, scanner endpoint/credential and explicit adopter retention policy. Secrets must remain outside version control.
- maintainer acceptance must exercise cross-tenant denial, unauthorized reviewer denial, scanner/storage/key failures, encrypted-at-rest inspection, access audit immutability, legal hold and restore.
- TRUST-002 remains incomplete until implementation, exact-head checks and maintainer acceptance are recorded. This ADR is not evidence that storage exists.

## Out of scope

Emergency response, AI adjudication, automatic severity decisions, a universal retention duration, a default cloud vendor, public/presigned downloads and automatic deletion without adopter policy.


## Project acceptance policy

No separate independent QA operator, acceptance worker, or external sign-off is required. The project maintainer accepts work after reviewing implementation, automated tests/evaluations, exact-head required CI/security checks, and recorded evidence. Technical gates remain mandatory; merge alone is not acceptance.
