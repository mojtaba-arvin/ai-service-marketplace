# M7 and M9 — Trust, Safety and Support Specification

**Feature IDs:** TRUST-001–TRUST-004, SUP-001–SUP-002  
**Status:** support case lifecycle and append-only support action audit are implemented. TRUST-002 incident intake/reviewer workflow and the first private evidence upload/retrieval slice are implemented; complete retention/legal-hold administration, operational recovery and independent acceptance remain incomplete. Dispute workflow and privacy-contact capabilities remain incomplete.

## Ownership

- **Professional verification:** verification case lifecycle, private evidence references, reviewer decisions and append-only review events. Implementation: `professional_verification.go`, migration `020_professional_verification.sql`.
- **Support:** requester-owned cases, tenant-scoped support agents, assignment, guarded status transitions and append-only action events. Implementation: `support_cases.go`, migration `021_support_cases.sql`. Creation, assignment and status changes record actor, action, reason and before/after JSON in the same transaction; PostgreSQL rejects UPDATE/DELETE on event rows. This implements SUP-001 and SUP-002.
- **Incident/safety reports:** `incident_reports` and `incident_report_events` own order-bound intake, reporter reads and the explicit `incident:review` workflow (migrations 033–034; `incidents.go`). Evidence bytes use the replaceable private storage interface in `evidence.go`, encrypted filesystem adapter, API-mediated reporter/reviewer retrieval, ClamAV scanning and append-only access events (migration 035; #217). Retention expiry denies retrieval but does not delete bytes; legal-hold administration, audited deletion and backup coordination remain open.
- **Dispute adjudication and privacy-preserving contact:** remain separate backlog capabilities until their own policy, data and workflow contracts are approved. They must not be inferred from support cases or order cancellation.

## Data and evidence boundaries

Verification evidence is represented by a private object reference, not a public URL or a document body. The storage adapter must enforce private access, short-lived retrieval authorization, encryption, malware/content validation, retention and deletion policy. A syntactically valid `private://` reference is not proof that storage access controls are implemented.

Support case records and events are tenant-scoped. Support assignment must target an active support-agent record in the same tenant. Support actions must be attributable and append-only.

## Background checks and periodic review

TRUST-008 extends professional verification with market- and service-risk-specific background-check adapters, consent/evidence minimization, expiry, re-review and deterministic dispatch blocking for failed or expired checks. The platform does not invent check providers or legal criteria; adopters configure approved sources and policy.

## Invariants

1. A professional can submit evidence only for their own profile in their tenant.
2. An operator cannot review their own verification case.
3. A decision requires a valid state and a non-empty reason; approval changes provider verification state transactionally with the audit event.
4. A support actor must have both a support role and an active `support_agents` record for the case tenant.
5. Customers/professionals can read only their own support cases; support staff can read only cases in their active tenant scope.
6. Case creation, assignment and status transitions append audit events in the same transaction.
7. Closed cases cannot be silently reopened; transitions must follow the documented state machine.
8. Safety incidents and disputes require human-owned decisions and must never be auto-resolved by AI.

## Failure behavior

Unknown evidence references, duplicate open verification cases, cross-tenant access, self-review, inactive support agents, invalid state transitions and missing decision reasons fail closed. Sensitive evidence must never appear in ordinary application logs or public API payloads.

## Verification

- Verification: `professional_verification_test.go`
- Support: `support_cases_test.go`
- Support audit storage: `021_support_cases.sql` enforces append-only event rows with a database trigger.
- Required negatives: wrong profile owner, cross-tenant reviewer, self-review, duplicate submission, inactive support actor, cross-tenant support read, unassigned support user, invalid transition and immutable audit-event mutation.

### Dispute Triage AI boundary

The `internal/disputetriage` package provides an advisory case summary, established-fact list, missing-information checklist and possible next-step options. Case reason and retrieved evidence are redacted before model invocation. Evidence retrieval is scoped by market, locale and dispute type; option citations must reference only retrieved evidence. Strict output rejects unknown fields, including verdict, refund amount, settlement or authority-bearing commands.

Every result sets `requires_human_adjudication=true`. AI cannot determine liability, issue refunds, settle a case, change order state or replace an adjudicator. The versioned golden set checks critical evidence coverage. This package is a service boundary only; dispute persistence, evidence storage/access policy, API/UI, appeal workflow and independent adjudicator integration remain open. No evidence-retention period is inferred.

## Open gaps

- Complete the private evidence storage/retrieval and access-audit boundary in #217. Retention/legal-hold behavior must be adopter-configured; do not invent a universal duration or enable automatic deletion without an explicit policy.
- Define dispute states, roles, evidence access, appeal and resolution authority before implementing TRUST-003.
- Define contact relay, disclosure controls, abuse reporting and retention before implementing TRUST-004.
- Complete audited retention/legal-hold administration, deletion and backup-expiry coordination; prove encryption-key rotation/restore and scanner/storage failure recovery. Do not infer that expiry deleted bytes or that legal hold is operational.
