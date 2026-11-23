# AI Service Marketplace

An open-source, self-hostable platform for building and operating a complete, multi-market home-services marketplace. It serves customers, independent professionals, operators, support and finance teams, property managers, and developers integrating external systems.

This is a full-product software project—not a demo, an MVP, or a repository whose intended scope ends at a customer-to-professional vertical slice. Adopters should be able to configure their own market, select providers, deploy the platform, and extend it through documented module and adapter contracts.

## Complete product scope

The product is organized into fourteen first-class domains:

| Domain | Capabilities |
|---|---|
| M1 Identity & accounts | OTP and optional email/social sign-in; customer, professional, admin, support, finance and B2B roles; profiles, addresses, devices, sessions, trusted devices, abuse controls, privacy export/deletion |
| M2 Catalog & services | Hierarchical admin-defined taxonomy, dynamic service forms, versioned price books/rules, packages/subscriptions, localized SEO content |
| M3 Customer experience | Conversational/form/photo intake, scheduling, explained estimates, live tracking/ETA, masked chat/calls, work approval, variations, invoices, reviews, history/reorder, cancellation/rescheduling |
| M4 Professional workspace | Onboarding, KYC, skill assessment/academy, calendar/coverage, offers, quotes, wallet/settlement, business tools, offline-critical workflows, quality coaching |
| M5 Matching & dispatch | Eligibility/ranking, broadcast/sequential/customer-choice strategies, reassignment, capacity controls, demand/supply forecasting |
| M6 Payments & finance | Immutable double-entry ledger, idempotency/replay, escrow, PSP adapters, commissions/incentives/credits, payouts, refunds, chargebacks, invoices, tax reports, reconciliation, fraud controls |
| M7 Trust & safety | Identity/credential review, background-check adapters, SOS/trip sharing, insurance/claims, disputes/evidence/human adjudication, suspicious-behavior signals, moderation, immutable audit |
| M8 Communications | Push/SMS/email and market channels, templates/preferences, transactional/marketing consent, protected in-app chat, ticketing and AI-assisted support |
| M9 Admin & operations | Order/customer/pro/market management, guarded state-machine interventions, dispute/finance queues, pricing/campaign/policy/feature flags, dashboards, granular RBAC and step-up approval |
| M10 Growth | Service×region SEO, content, referrals, coupons/campaigns, landing pages, attribution, A/B tests and lifecycle messaging |
| M11 B2B | Organizations, multiple properties, maintenance contracts, consumption reports, consolidated invoices, building-manager API/webhooks |
| M12 Data & analytics | Versioned events, warehouse/export, dashboards, experiments, leakage and quality monitoring |
| M13 Platform & compliance | Country/language/currency/tax/RTL configuration, i18n, adapters, observability, secret management, backup/DR, consent and privacy |
| M14 AI capabilities | Intake Copilot, Pro Copilot, Dispute Triage, Support Copilot, explainable leakage/fraud, quality coaching, demand/ETA/price forecasting |

See [Product Scope](docs/product-scope.md) for the complete capability catalogue and [Engineering Roadmap](docs/ROADMAP.md) for dependency-aware delivery. Implementation status belongs to [feature_list.json](feature_list.json), module specifications, issues and [Development Progress](docs/progress.md). A planned capability is not implemented merely because it is documented; a merged PR is not independent acceptance.

## Current repository status

The repository already contains a Go/PostgreSQL modular-monolith API, customer/professional/operator browser workflows, migrations, transactional outbox foundations, ledger primitives, market configuration, B2B organization/property foundations, deployment assets and automated CI/security workflows. These are foundations for the complete product, not evidence that all fourteen domains are complete or that a public production service is operating.

## Architecture

- Transactional core: Go modular monolith and PostgreSQL/PostGIS.
- Customer web: Next.js + TypeScript.
- Operations UI: React + Vite + TypeScript.
- Mobile: Flutter customer and professional clients.
- AI/data workloads: isolated Python workers behind an internal AI gateway.
- Contracts: OpenAPI for HTTP APIs and JSON Schema for events.
- Async/realtime: transactional outbox; NATS JetStream and Valkey where justified.
- Deployment: Docker Compose and containerized self-hosting.

This is the current repository baseline. The alternative TypeScript end-to-end stack in the original proposal conflicts with code already integrated here; changing core language, ORM, queue or mobile framework requires an explicit ADR and migration plan, not an undocumented rewrite. See [Technical Plan](docs/vision/tech-plan.md).

## Getting started

See [Deployment](infra/DEPLOYMENT.md). Start from .env.example and never commit production credentials.

```sh
make verify
make verify-full
```

These commands verify the current repository state; they do not certify every planned capability.

## Extensibility and safety

1. Market-specific behavior is configuration plus replaceable adapters, not scattered country conditionals.
2. External adapters have deterministic local fakes and contract tests; CI needs no live provider credentials.
3. Money uses integer minor units and an immutable, idempotent, balanced double-entry ledger. Corrections use reversals.
4. Cross-module interaction uses documented public APIs and versioned events.
5. AI affecting money, identity, safety, access or reputation remains advisory until deterministic policy and/or authorized human approval.
6. PII is minimized and redacted before external calls; consent, retention and deletion are explicit.
7. Features are accepted only with required automated and independent evidence.

## Contributing

Read [AGENTS.md](AGENTS.md), [Contributing](CONTRIBUTING.md), [module specifications](docs/specs/README.md), [ADRs](docs/adr/) and the [roadmap](docs/ROADMAP.md). Choose a scoped issue, preserve module ownership, add failure-path tests and submit a focused PR. See [Security](SECURITY.md).

## License

MIT. See [LICENSE](LICENSE).
