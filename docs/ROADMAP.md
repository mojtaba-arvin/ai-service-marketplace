# Full Product Engineering Roadmap

This roadmap delivers the complete open-source home-services marketplace defined in [Product Scope](product-scope.md). It is not an MVP definition, commercial launch calendar or restriction of product scope. Stages order dependencies and risk; every capability remains part of the intended product.

The issue tracker and feature_list.json are execution/acceptance sources of truth. This document groups work into outcomes.

## Outcomes

1. Adopters can configure, self-host, operate and extend the platform without mandatory vendor lock-in.
2. Customers and professionals complete the service lifecycle across web and mobile.
3. Operators, support and finance teams manage the marketplace safely and audibly.
4. The platform supports B2B/property maintenance and multiple markets through configuration/adapters.
5. AI improves intake, professional execution, support, trust and forecasting without taking consequential authority.
6. Data, security, privacy, reliability and operations are production-grade and verifiable.

## Workstreams

### W0 — Open-source engineering factory
Reproducible local environment; deterministic verification and full integration suite; module boundaries/ADRs/playbooks; OpenAPI/JSON Schema validation and client generation; test fixtures/fakes/migration tooling; CI/security/dependency/secret checks; issue/PR templates and release automation.
**Exit:** a new contributor can clone, bootstrap, verify and run the platform from documented steps.

### W1 — Identity, access and privacy (M1)
OTP and replaceable delivery adapters; optional email/Apple/Google identity; customer/pro/admin/support/finance/B2B roles; profiles, addresses, devices, sessions, trusted devices and abuse controls; consent, export, deletion/anonymization and audited access.
**Gate:** tenant isolation, session rotation/revocation, abuse tests and privacy lifecycle independently verified.

### W2 — Catalog, service design and pricing (M2)
Admin-defined taxonomy/services; dynamic questionnaires; versioned price menus, multipliers and rules; immutable order price snapshots; packages/subscriptions; localized service×region SEO with reviewable AI drafting.
**Gate:** published versions immutable; historical orders retain price basis; forms are schema-validated.

### W3 — Customer and professional experiences (M3–M4)
Conversation/form/photo intake; structured extraction and clarifying questions; scheduling/urgency/estimates; tracking/ETA/masked communication; completion/variations/invoices/reviews/history/reorder/cancellation; professional onboarding/KYC/skills/academy/calendar/areas/offers/quotes/earnings/CRM/reports/offline workflows.
**Gate:** customer and professional web/mobile journeys, permissions, retries, offline sync and negative paths pass E2E.

### W4 — Matching, dispatch and capacity (M5)
Explainable eligibility/ranking; broadcast/sequential/customer-choice; expiry/timeout/reassignment; cancellation recovery; capacity controls; demand/supply shortage forecasts.
**Gate:** concurrency, fairness policy, stale offers, reassignment and capacity invariants pass workload tests.

### W5 — Payments, ledger and finance (M6)
Immutable balanced ledger; idempotent posting/replay/reversals; escrow/holds; commissions/rewards/penalties/credits; PSP adapters; payouts/refunds/chargebacks; invoices/tax/reconciliation; explainable payment fraud.
**Gate:** property-based balance tests, duplicate/timeout/replay recovery, reconciliation and independent Finance QA. Real funds remain disabled until adopter/provider/legal approvals.

### W6 — Trust, safety and communications (M7–M8)
KYC/credentials/background checks; periodic review/blocklists; SOS/trip sharing/emergency; insurance/claims; disputes/evidence/AI triage/human adjudication; moderation/audit; push/SMS/email/channels/preferences/consent/chat; support tickets/knowledge/AI suggestions.
**Gate:** least privilege, human escalation, audit and failure-safe notification behavior.

### W7 — Admin, operations and growth (M9–M10)
Guarded order state machine and work queues; people/market/finance/dispute management; pricing/campaign/policy/feature flags; step-up approvals; operations/supply/finance dashboards; SEO/content/referral/coupons/attribution/A-B/lifecycle.
**Gate:** operator actions are permissioned, auditable and measurable.

### W8 — B2B and subscriptions (M11)
Organizations/memberships/properties; maintenance contracts/recurring work; consumption reporting/consolidated invoices; building-manager APIs/webhooks.
**Gate:** order/property attribution and finance/ledger gates pass before consolidated billing is enabled.

### W9 — Data and analytics (M12)
Canonical event taxonomy; compatibility policy; idempotent replayable projections; warehouse/export; funnel/supply/operations/finance/quality/experiment dashboards; leakage monitoring.
**Gate:** each metric has source, denominator, time window, privacy class and reproducible projection.

### W10 — Multi-market platform and compliance (M13)
Country/language/locale/currency/tax/timezone/RTL/address/hours/policy config; provider adapters; consent/privacy; secrets/KMS; observability; backup/restore/DR; audited feature flags and market activation.
**Gate:** configuration matrix, adapter contracts, privacy/security review and recovery rehearsal.

### W11 — AI product capabilities (M14)
Intake Copilot; Pro Copilot; Dispute Triage; Support Copilot; explainable Leakage/Fraud Detector; Quality Score/coaching; Demand/ETA/Price Forecast.
**Gate:** per-capability golden evaluations, privacy redaction, provenance, cost limits, safe fallback and deterministic/human authority controls.

## Parallel execution

Split work into small issues/PRs by module and file ownership. Independent workstreams proceed concurrently; shared contracts, migrations and high-risk state machines use explicit dependency gates. CI is asynchronous: while one PR checks, work proceeds on independent tasks. A PR opening or workflow start is never completion evidence.

## Acceptance rules

- Every capability maps to a feature ID, module spec, tests/evaluation and dependencies.
- Required checks pass on exact PR head and after rebase on current main.
- Security, finance, identity, privacy, B2B attribution and safety require negative-path and independent review.
- Do not invent policy/retention periods, legal rules, fairness thresholds or regulated providers.
- Only independent acceptance sets passes=true and verified_by.
- progress.md records evidence/blockers; roadmap records intended outcomes, not optimistic completion.

## Current baseline

The repository has a Go/PostgreSQL modular-monolith foundation, customer/professional/operator web workflows, transactional events/outbox, ledger primitives, market configuration, B2B organization/property foundations and deployment/CI assets. These foundations do not mean the full product is complete. See [progress](progress.md), [feature inventory](../feature_list.json), [module specs](specs/README.md) and [GitHub Issues](https://github.com/mojtaba-arvin/ai-service-marketplace).
