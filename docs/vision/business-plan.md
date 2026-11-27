# Product Vision — Open-Source Full-Scale Home Services Marketplace

## Product intent

Build and maintain a complete, open-source, self-hostable software platform that enables independent teams to create, operate and extend multi-market home-services marketplaces. It serves customers, professionals, marketplace operators, support and finance staff, property/building managers, and developers integrating external systems.

This is a product scope, not a market-entry plan. It is not limited to a demo, MVP, single vertical slice, one country or one vendor. The repository must progressively deliver the complete capabilities in [Product Scope](../product-scope.md), with truthful per-feature implementation and verification status.

## Actors and outcomes

- Customers discover services, describe work by conversation/form/media, understand estimates, schedule, communicate safely, track fulfillment, approve changes/completion, pay, review and reorder.
- Professionals onboard and verify identity/skills, define services/coverage, manage availability, receive and fulfill jobs, quote unknown work, track earnings/settlements, use business tools and improve quality.
- Operators/support configure the marketplace, manage people and orders, intervene in guarded workflows, handle disputes, campaigns, communications and operational health.
- Finance teams reconcile ledger, PSPs, escrow, refunds, chargebacks, invoices, tax records and professional settlements.
- Organizations/property managers manage members and properties, schedule maintenance, monitor consumption, receive consolidated billing and integrate through APIs/webhooks.
- Developers/adopters self-host, configure markets/adapters, use contracts, run deterministic tests and extend modules without forking core behavior.

## Fourteen product domains

1. **M1 Identity & accounts:** OTP and optional email/social providers; customer/professional/admin/support/finance/B2B roles; profiles, multiple addresses, devices, trusted devices, sessions, abuse controls, privacy export and deletion.
2. **M2 Catalog & service design:** admin-managed hierarchy, services, dynamic questionnaires, versioned prices/rules/multipliers, packages/subscriptions and localized SEO content.
3. **M3 Customer experience:** conversational/form/photo intake, structured extraction, clarifying questions, scheduling/urgency, explained estimates, live tracking/ETA, masked chat/calls, work approval, variations, invoices, bilateral reviews, history/reorder and policy-based cancellation/rescheduling.
4. **M4 Professional experience:** onboarding, KYC, skill exams/academy, calendar/areas, offers, structured quotes, wallet/settlement, CRM/invoices/reports, offline-critical workflows and quality coaching.
5. **M5 Matching & capacity:** explainable ranking by distance/skill/rating/availability/fairness, broadcast/sequential/customer-choice dispatch, reassignment, capacity controls and demand/supply forecasting.
6. **M6 Payments & finance:** immutable double-entry ledger, idempotency/replay, escrow/holds, PSP adapters, commissions/rewards/penalties/credits, payouts/refunds/chargebacks, invoices, tax reports, reconciliation and explainable fraud controls.
7. **M7 Trust & safety:** identity/credential/background checks, periodic review, SOS/trip sharing, insurance/claims, disputes/evidence/human adjudication, suspicious behavior detection, moderation and append-only audit.
8. **M8 Communications:** push/SMS/email and optional market channels, templates/preferences/delivery, transactional vs marketing consent, protected in-app chat, support tickets, knowledge base and AI assistance.
9. **M9 Admin & operations:** people/orders/market management, visible guarded state machine, controlled interventions, dispute/finance queues, price/campaign/policy/flag controls, dashboards, granular RBAC and step-up approval.
10. **M10 Growth:** service×region SEO, editorial content, referrals, coupons/campaigns, landing pages, attribution, A/B tests and lifecycle communication.
11. **M11 B2B:** organizations, memberships, multiple properties, maintenance contracts, consumption reports, consolidated invoices and building-manager APIs/webhooks.
12. **M12 Data & analytics:** canonical events, warehouse/export, operational/business dashboards, experiments, leakage and quality monitoring with explicit metric definitions.
13. **M13 Platform & compliance:** market configuration for country/language/currency/tax/locale/RTL/timezone/policies/providers, i18n, observability, secrets, backup/DR, consent/privacy and retention.
14. **M14 AI:** Intake Copilot, Pro Copilot, Dispute Triage, Support Copilot, explainable Leakage/Fraud Detector, Quality Score/coaching, Demand/ETA/Price Forecast.

## Architecture principles

- Complete product scope; incremental, dependency-aware delivery. Staging does not reduce intended scope.
- Modular monolith for transactional domains with explicit ownership and public APIs/events.
- Contract-first APIs/events: OpenAPI and JSON Schema are versioned sources of truth; generated clients are preferred.
- PostgreSQL is transactional source of truth; PostGIS supports geographic queries.
- Outbox events commit atomically; consumers are idempotent and tolerate retries/reordering.
- Market behavior is configuration plus replaceable adapters, not country-specific code forks.
- External providers have local fakes, contract tests and explicit capabilities.
- Money uses integer minor units and immutable balanced double-entry entries; corrections use reversals.
- Human authority and deterministic policy govern money, safety, identity, disputes, access and reputation.
- AI is eval-first, observable, cost-bounded, privacy-aware and provider-neutral.
- Privacy, accessibility, localization, RTL, self-hosting and recovery are product capabilities.

## AI authority boundary

AI may classify, summarize, extract, recommend, draft, forecast and coach. It does not autonomously capture/release money, approve KYC, decide safety/emergency outcomes, resolve disputes, change reputation or take consequential action. These require deterministic policy and/or authorized human approval with provenance and audit evidence.

## Market and regulatory boundary

Market configuration represents country, currency, tax, language/direction, locale, timezone, address format, work hours, cancellation policy and provider capabilities. Armenia and GCC fixtures may exercise configuration but do not imply launch priority, legal conclusions or approval to activate regulated services. Production PSP/KYC/tax/background-check activation requires adopter credentials, local review and explicit operator approval.

## Definition of product completeness

Each capability requires a module specification and acceptance criteria; migrations/public contracts/authorization where applicable; success, failure, concurrency, retry and negative-path tests; fake adapters and contract tests; accessible/localized user workflows; observability, runbooks, privacy/security controls and deployment docs; and maintainer acceptance evidence before feature status is marked passing.

Current implementation and gaps are tracked in [feature_list.json](../../feature_list.json), [module specifications](../specs/README.md), [GitHub Issues](https://github.com/mojtaba-arvin/ai-service-marketplace/issues) and [Development Progress](../progress.md). A roadmap stage or PR is not proof of completion.

## Non-goals

The repository does not choose an adopter's commercial model, commission rates, launch country/date, legal position or vendor contract. These remain adopter decisions and market-specific configuration, not hard-coded assumptions.


## Project acceptance policy

No separate independent QA operator, acceptance worker, or external sign-off is required. The project maintainer accepts work after reviewing implementation, automated tests/evaluations, exact-head required CI/security checks, and recorded evidence. Technical gates remain mandatory; merge alone is not acceptance.
