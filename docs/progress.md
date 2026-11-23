# Development Progress

> Rolling evidence snapshot: 2026-11-23. Git history and PRs retain detailed history; keep this file concise. Never mark a feature QA-passing without independent evidence.

## Product direction

AI Service Marketplace is a complete open-source, self-hostable, multi-market home-services platform spanning M1–M14. It is not an MVP, demo-only product or single vertical slice. Product intent is in [README](../README.md), [Product Scope](product-scope.md), [Business Vision](vision/business-plan.md), [Technical Plan](vision/tech-plan.md) and [Roadmap](ROADMAP.md).

## Integrated on main

- Go/PostgreSQL modular-monolith API; Next.js customer web; React/Vite operator UI; Flutter mobile direction; isolated Python AI workloads.
- Identity/tenant memberships, customer order workflows, professional profiles/eligibility, scheduling, availability, dispatch offers, cancellation, order history and transactional outbox.
- Immutable ledger primitives, payment adapter boundaries, market configuration/localization, provider eligibility and B2B organization/property membership foundations.
- One-shot migration runner, container/Compose deployment, Caddy ingress, browser E2E, PostgreSQL backup/restore rehearsal, CI, CodeQL, Dependency Review and Agent Guard.
- PR #156 merged: full fourteen-domain product definition, adopter-facing README, business/technical vision and dependency-aware roadmap.
- PR #159 merged: grounded Professional Copilot service boundary with redaction, retrieval-only citations, strict schema/provenance, mandatory professional review and offline golden evaluation.
- PR #162 merged: platform-only global market configuration mutation, transactional before/after audit snapshots and append-only audit history including TRUNCATE protection.
- PR #168 merged: bounded dynamic service-form schema/answer validation with strict typed constraints; publication, persistence, API/UI and order snapshots remain open.

These are repository capabilities, not evidence that every domain is complete or that a production marketplace is publicly deployed.

## Active implementation and backlog PRs

- #172: session-bound device inventory/revocation; migration 025 is ordered before 026. Required checks and independent acceptance are pending. This does not claim hardware attestation or trusted-device status.
- #165: expand feature inventory from 44 coarse records to 77 decomposed capabilities; all new records remain passes=false.
- #166: bounded dynamic service-form schema/answer validation; persistence, admin publication, API/UI and order snapshots remain follow-up work.

PR creation or a passing unit test alone does not establish feature acceptance.

## Product workstreams and gates

1. **M1 Identity/privacy:** finish device inventory, then define explicit trusted-device/step-up policy; implement privacy export/deletion only under approved policy.
2. **M2 Catalog/pricing:** dynamic form version persistence/publication, immutable price rules/snapshots, packages and subscriptions, localized SEO.
3. **M3/M4 Customer/professional:** full intake, scheduling, estimates, tracking, masked communications, completion/variations/reviews/reorder; onboarding/KYC/academy, quotes, earnings, CRM and offline-safe mobile.
4. **M5 Matching:** broadcast/sequential/customer-choice strategies, reassignment, capacity and forecast signals.
5. **M6 Finance:** escrow/holds, payouts/refunds/chargebacks, tax/invoices/reconciliation; keep gated on independent Order/Ledger QA.
6. **M7/M8 Trust and communications:** SOS/claims/disputes/moderation/audit, consent-separated channels, protected chat and support copilot.
7. **M9/M10 Admin and growth:** guarded state machine, step-up approval, queues/dashboards, SEO/referrals/campaigns/experiments/lifecycle.
8. **M11/M12 B2B and data:** property-attributed orders/ledger, contracts/webhooks, canonical events, replayable projections and warehouse/dashboards.
9. **M13/M14 Platform and AI:** market/adapters, privacy/DR/observability, plus Intake/Pro/Dispute/Support/Fraud/Quality/Forecast AI capabilities.

## Hard blockers and safety boundaries

- #110 remains gated on external consumer compatibility; #133 on executable analytics projector/generation semantics; #137 on explicit canonical event payload compatibility.
- B2B billing waits for independent Order/Ledger gates (#57/#10) and canonical event readiness (#87).
- No retention duration, privacy policy, fairness threshold, tax/legal rule or regulated-provider approval is invented.
- GitHub security settings require owner-side confirmation; do not change repository security controls without explicit approval.
- AI affecting money, safety, identity, access or reputation remains advisory until deterministic policy and/or authorized human approval.
- Fake adapters are for local/CI only; no real payment capture, KYC or production messaging is implied.

## Acceptance and execution rules

- feature_list.json is the structured feature inventory; new capabilities stay passes=false until required implementation, negative-path tests, exact-head CI and independent acceptance evidence exist.
- Keep module ownership and public API/event boundaries; use small independent PRs and work on other tasks while CI runs.
- Required checks must pass on the current PR head and current main base. Do not bypass branch protection, security gates or independent QA.
- progress.md records current evidence; roadmap records intended outcomes. Neither substitutes for issue-level acceptance.
- Local GitHub access from this runtime was unavailable when tested (DNS resolution failure); GitHub-hosted CI is used as the execution evidence. No local test success is claimed.
