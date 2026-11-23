# M7 and M9 — Trust, Safety and Support Specification

**Feature IDs:** TRUST-001–TRUST-004, SUP-001–SUP-002  
**Status:** provider verification and support-case foundations exist; incident, dispute and privacy-contact capabilities remain incomplete.

## Ownership

- **Professional verification:** verification case lifecycle, private evidence references, reviewer decisions and append-only review events. Implementation: `professional_verification.go`, migration `020_professional_verification.sql`.
- **Support:** requester-owned cases, tenant-scoped support agents, assignment, guarded status transitions and append-only action events. Implementation: `support_cases.go`, migration `021_support_cases.sql`.
- **Incident/safety reports, dispute adjudication, privacy-preserving contact:** backlog only until their own policy, data and workflow contracts are approved. They must not be inferred from support cases or order cancellation.

## Data and evidence boundaries

Verification evidence is represented by a private object reference, not a public URL or a document body. The storage adapter must enforce private access, short-lived retrieval authorization, encryption, malware/content validation, retention and deletion policy. A syntactically valid `private://` reference is not proof that storage access controls are implemented.

Support case records and events are tenant-scoped. Support assignment must target an active support-agent record in the same tenant. Support actions must be attributable and append-only.

## Invariants

1. A professional can submit evidence only for their own profile in their tenant.
2. An operator cannot review their own verification case.
3. A decision requires a valid state and a non-empty reason; approval changes provider verification state transactionally with the audit event.
4. A support actor must have both a support role and an active `support_agents` record for the case tenant.
5. Customers/professionals can read only their own support cases; support staff can read only cases in their active tenant scope.
6. Case assignment and status transitions append audit events in the same transaction.
7. Closed cases cannot be silently reopened; transitions must follow the documented state machine.
8. Safety incidents and disputes require human-owned decisions and must never be auto-resolved by AI.

## Failure behavior

Unknown evidence references, duplicate open verification cases, cross-tenant access, self-review, inactive support agents, invalid state transitions and missing decision reasons fail closed. Sensitive evidence must never appear in ordinary application logs or public API payloads.

## Verification

- Verification: `professional_verification_test.go`
- Support: `support_cases_test.go`
- Required negatives: wrong profile owner, cross-tenant reviewer, self-review, duplicate submission, inactive support actor, cross-tenant support read, unassigned support user, invalid transition and immutable audit-event mutation.


### Dispute Triage AI boundary

The new `internal/disputetriage` package provides an advisory case summary, established-fact list, missing-information checklist and possible next-step options. Case reason and retrieved evidence are redacted before model invocation. Evidence retrieval is scoped by market, locale and dispute type; option citations must reference only retrieved evidence. Strict output rejects unknown fields, including verdict, refund amount, settlement or authority-bearing commands.

Every result sets `requires_human_adjudication=true`. AI cannot determine liability, issue refunds, settle a case, change order state or replace an adjudicator. The versioned golden set checks critical evidence coverage. This package is a service boundary only; dispute persistence, evidence storage/access policy, API/UI, appeal workflow and independent adjudicator integration remain open. No evidence-retention period is inferred.

## Open gaps

- Define incident severity, triage, escalation, emergency handling and evidence-retention policy before implementing TRUST-002.
- Define dispute states, roles, evidence access, appeal and resolution authority before implementing TRUST-003.
- Define contact relay, disclosure controls, abuse reporting and retention before implementing TRUST-004.
- Replace reference-only evidence handling with a private storage adapter and explicit authorization contract.
