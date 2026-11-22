# Engineering Roadmap

This roadmap describes the long-term engineering direction of the open-source AI Service Marketplace. It is a capability and quality roadmap—not a commercial launch schedule, country rollout plan, or promise that every integration will be enabled.

The issue tracker and `feature_list.json` remain authoritative for scoped implementation work and feature-level acceptance. This document groups that work into coherent engineering outcomes.

## Guiding outcomes

1. A contributor can run, understand, test, and self-host the platform using documented, reproducible steps.
2. Customers, professionals, and operators can complete the core service lifecycle with strong authorization and concurrency guarantees.
3. Financial, trust, privacy, and support workflows are auditable and safe by construction.
4. Integrations are replaceable, contract-tested, and optional; the core platform does not depend on one vendor.
5. Market-specific behavior is configuration-driven and testable without treating example countries as production approvals.
6. AI capabilities are bounded, evaluated, observable, and never authoritative for consequential decisions.

## Roadmap stages

### Stage 0 — Open-source foundation

**Outcome:** maintainable repository and safe contribution workflow.

- License, architecture decisions, agent/contributor guidance, issue and PR templates.
- Deterministic local verification and CI without live third-party credentials.
- Protected default branch, dependency/security checks, secret hygiene.
- Reproducible development environment and documented self-hosting baseline.

**Exit gate:** a clean contributor environment can run the documented verification commands; required CI is green; security controls and ownership boundaries are documented.

### Stage 1 — Core marketplace vertical slice

**Outcome:** a complete, reproducible customer-to-professional workflow.

- Identity and role-scoped authorization.
- Catalog, pricing, scheduling, orders, dispatch candidates, expiring offers, and acceptance.
- Customer, professional, and operator browser workflows.
- Explicit state transitions, idempotency, concurrency handling, and transactional events.
- One non-production demonstration configuration for end-to-end testing.

**Exit gate:** independent end-to-end and negative-path QA verifies the workflow, including cross-account access, duplicate requests, stale/expired offers, concurrent acceptance, and recovery from dependency failures. No test-only adapter is exposed as a production integration.

**Current tracking:** [#53](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/53), [#56](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/56), and [#57](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/57).

### Stage 2 — Trust, support, and operational completeness

**Outcome:** operators can handle exceptions without unsafe manual database intervention.

- Provider verification and evidence lifecycle.
- Support case ownership, scoped access, and append-only action audit.
- Dispute and incident workflows with explicit human decisions.
- Privacy-preserving contact and retention controls.
- Notification preferences, delivery states, retries, and provider adapters.
- Operator permissions and bounded audit search.

**Exit gate:** sensitive actions are permissioned and audited; privacy/retention behavior is tested; external adapters have offline fakes and contract suites; failure and retry behavior is observable.

### Stage 3 — Financial correctness and provider integration

**Outcome:** financial capabilities are correct independently of any specific payment provider.

- Immutable, balanced, idempotent ledger postings.
- Compensating reversals and reconciliation workflows.
- Versioned payment, refund, and settlement adapter contracts.
- Explicit handling of timeouts, duplicate callbacks, partial failure, and provider reconciliation.
- Clear separation between simulated/test adapters and real provider integrations.

**Exit gate:** financial invariants and reconciliation are independently verified; provider contracts pass deterministic tests; production enablement remains an explicit operator decision after applicable legal, security, and provider review. This stage does not presume any specific country or provider.

### Stage 4 — Multi-market and organization capabilities

**Outcome:** the same codebase supports distinct configurations and organization workflows without scattered conditionals.

- Versioned market configuration for currency, locale, timezone, language/direction, policies, and provider capabilities.
- Explicit feature gating and audited configuration changes.
- Organization membership, property/location registry, and consolidated billing records.
- Compatibility tests across representative configurations, including Armenia and GCC examples where useful.
- Data isolation and migration tests for tenant/organization boundaries.

**Exit gate:** unknown or incomplete configuration fails closed; configuration changes are auditable; cross-market and cross-organization isolation tests pass. Regional examples are test inputs, not launch commitments.

### Stage 5 — Analytics, performance, and platform scale

**Outcome:** engineering decisions are based on reliable operational evidence.

- Canonical, versioned event envelope and compatibility checks.
- Reproducible funnel, supply, reliability, and service-quality metrics.
- Structured logs, metrics, traces, alerting, and operational runbooks.
- Workload-based load tests, query budgets, resource budgets, and measured optimization.
- Extract services or introduce infrastructure only when profiling and ownership boundaries justify it.

**Exit gate:** metrics have explicit sources, denominators, and time windows; representative workloads and recovery objectives are documented; scaling changes are supported by measurements.

### Stage 6 — Evaluated AI capabilities

**Outcome:** AI improves user and operator workflows without taking authority away from deterministic policy or humans.

- Internal AI gateway with provider abstraction, redaction, schema validation, cost controls, and observability.
- Versioned golden datasets and repeatable evaluations.
- Intake classification, request summarization, quote drafting, and support assistance.
- Confidence/provenance reporting and safe fallbacks.
- Human review and deterministic policy gates for identity, safety, reputation, disputes, and money.

**Exit gate:** evaluation thresholds are versioned and enforced in CI; critical regressions fail checks; unsafe or malformed model output cannot trigger consequential actions.

## Cross-cutting requirements

Every stage must preserve:
- modular ownership and public contracts;
- secure-by-default authorization and secret handling;
- migration safety and backward-compatible interface evolution;
- idempotency, bounded retries, and explicit failure semantics;
- accessible, responsive, localizable user interfaces;
- reproducible tests and documented operational behavior;
- independent acceptance evidence before a feature is marked complete.

## How to use this roadmap

- Select work from the existing feature inventory and GitHub issues; create a focused issue when a deliverable is missing.
- Keep implementation PRs small and tied to acceptance criteria.
- Update this roadmap only when the architecture or stage boundaries materially change.
- Record actual progress in [Development Progress](progress.md), not by moving roadmap checkboxes optimistically.
- Do not convert geographic context into a commercial launch commitment or enable regulated integrations by implication.
