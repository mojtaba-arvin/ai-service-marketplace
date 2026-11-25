# ADR 0008 — Incident and safety report intake

- **Status:** Accepted
- **Date:** 2026-11-25
- **Scope:** M7 / TRUST-002

## Context

The marketplace needs a restricted way for customers and professionals to report safety incidents associated with an order. A support ticket or order cancellation is not a substitute for an incident record. The platform does not operate an emergency response center, and no adopter-specific emergency provider, jurisdictional rule or evidence-retention period is currently configured.

## Decision

1. **Intake is not emergency response.** The product must clearly state that it is not monitored as an emergency service and that a person in immediate danger must contact the local emergency services directly. Do not imply real-time monitoring, guaranteed response times, emergency dispatch or automatic intervention.
2. **Order-bound reporter authorization.** A customer may report only against an order they own in the same tenant. A professional may report only against an order to which their identity is assigned in that tenant. Tenant identity is derived from the persisted order, never from request input. A report cannot be attached to an unrelated or tenant-mismatched order.
3. **Minimal structured intake.** Store incident category, reporter narrative, occurrence time (if supplied), optional severity estimate, order reference and opaque private evidence references. Validate and bound all user-controlled fields. Do not store evidence bytes, public URLs, phone numbers or access tokens in the incident row.
4. **Severity is a triage hint, not an automated decision.** The reporter may choose `unknown`, `low`, `high` or `critical`; reviewers may correct the triage classification with a reason. No severity value automatically changes an order, blocks a professional, initiates a refund, contacts emergency services or determines liability.
5. **Human-owned workflow.** Initial states are `submitted`, `triaged`, `investigating`, `resolved` and `closed`. Only an authorized, active same-tenant safety reviewer may assign, triage, change severity, or close a report. Every reviewer decision requires a reason. The reporter may read their own report; other customers/professionals cannot. Support agents do not gain access merely from their support role; incident-review permission must be explicit in the safety policy.
6. **Append-only audit.** Creation, reviewer assignment, severity changes and state transitions append actor/action/reason/before/after events in the same transaction as the state change. Audit events reject UPDATE and DELETE at the database boundary. Invalid transitions and cross-tenant access fail closed.
7. **Evidence is a separate security boundary.** This feature accepts opaque references only. Evidence retrieval requires a separate private object-storage adapter with authorization, short-lived access, encryption, malware/content validation, access audit and deletion controls. Do not expose references as downloadable URLs or claim storage security from URI syntax alone.
8. **Retention is adopter-configured.** Do not invent a universal retention duration. Records and evidence follow an explicit adopter policy and must not be silently deleted while subject to a legal hold or open investigation. Automated deletion is out of scope until retention, legal hold and deletion semantics are specified.
9. **AI is advisory only.** No AI classification or summary may determine severity, liability, safety outcome, report closure, order state or financial remedy. Human reviewers retain authority.

## Consequences

- TRUST-002 can deliver order-bound intake and a restricted human review queue without claiming to provide emergency response or evidence storage.
- The feature requires a dedicated incident schema and audit stream; existing support cases/events must not be repurposed.
- Active reviewer membership/permission and evidence retrieval require explicit enforcement. The operator-role-only authorization model is not sufficient.
- API responses and logs must redact narrative/evidence references where not required; evidence references must never be copied into ordinary logs.
- The feature remains `passes=false` until positive/negative PostgreSQL tests, exact-head CI and independent acceptance are complete.

## Implementation sequence

1. Add tenant-bound incident records and append-only report events.
2. Implement order-participant intake and reporter-only reads.
3. Implement explicit same-tenant reviewer authorization, assignment, triage and guarded state transitions.
4. Add privacy/redaction, concurrency and cross-tenant tests.
5. Specify and integrate the private evidence adapter and adopter-configured retention separately; do not mark those capabilities complete until delivered.


## Reviewer workflow implementation contract — 2026-11-25

- The first reviewer implementation uses the dedicated operator grant action `incident:review`. An operator must have an active membership and an explicit grant for this exact action in the report's tenant. The generic `support` role, support-case access, or operator membership alone never authorizes incident review.
- A reviewer may list/read reports only within the actor's tenant. Assignment targets must also be active same-tenant operators with an active `incident:review` grant. Self-assignment is allowed; self-grant and cross-tenant assignment are not.
- Allowed status transitions are sequential and forward-only: `submitted → triaged → investigating → resolved → closed`. No reopen or skipped-state transition is exposed in this slice.
- Severity changes are allowed only while the report is not closed and always require a reason. Assignment changes are also prohibited after closure. Severity remains advisory and does not trigger an order, payment, provider, or emergency action.
- Each reviewer mutation writes a same-transaction append-only event with actor, reason and before/after state before updating the report. The database trigger permits a report update only when a matching event exists in the same transaction; identity, reporter narrative, order binding, occurrence time and evidence references remain immutable.
- Reviewer responses may expose opaque evidence identifiers but must not expose a public/download URL. Evidence retrieval remains unavailable until the separate private storage adapter and access-audit boundary are delivered.
