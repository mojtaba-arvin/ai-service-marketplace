# AI Service Marketplace

An open-source, multi-market home-services marketplace connecting customers with vetted independent professionals. The platform is designed around managed service quality, trusted transactions, recurring services, and auditable operations.

## Project status

The repository has moved beyond the initial foundation stage. The Go/PostgreSQL backend baseline is integrated into `main`, and the MVP release is being assembled around one complete customer-to-professional workflow.

The current MVP scope is tracked in [MVP Release #53](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/53). The release is not considered ready for public use until the production API, browser experience, deployment, and end-to-end acceptance criteria are complete.

### Integrated backend capabilities

- Tenant-scoped identity and session primitives.
- Market configuration and market-visible service catalog.
- Versioned service pricing and scheduled customer orders.
- Provider profiles, verification, service areas, and availability.
- Deterministic dispatch candidates, expiring offers, and atomic provider acceptance.
- Order history and lifecycle events.
- Double-entry ledger primitives and payment-provider contracts (no real payment capture or funds movement).
- Transactional outbox and background workers.
- Support and AI intake foundations.

### MVP delivery status

- Backend integration baseline: merged.
- Single-host container and HTTPS deployment foundation: merged.
- Production-safe customer/professional/operator API workflow: in progress.
- Responsive customer/professional/operations browser experience: implemented in the MVP release candidate; CI and integration acceptance pending.
- Host provisioning and deployed acceptance: tracked in [#56](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/56) and [#57](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/57).

See the [deployment guide](infra/DEPLOYMENT.md) for the current container topology, configuration requirements, seed procedure, and known limitations. It is a deployment foundation, not a claim that the complete MVP is already live.

## Architecture at a glance

- **Core API:** Go, with explicit modular-monolith boundaries.
- **MVP browser application:** Next.js + TypeScript, with customer, professional, and operator workspaces.
- **Dedicated operations console:** React + Vite + TypeScript (post-MVP).
- **Mobile clients:** Flutter (customer and professional applications; post-MVP).
- **AI workloads:** isolated Python workers behind an internal AI gateway.
- **Data:** PostgreSQL + PostGIS; pgvector only for use cases that justify vector retrieval.
- **Async and realtime infrastructure:** transactional outbox, NATS JetStream, and Valkey where caching or ephemeral coordination is justified.
- **Contracts:** OpenAPI and JSON Schema as versioned interface contracts.

See [the business vision](docs/vision/business-plan.md), [technical plan](docs/vision/tech-plan.md), [architecture decisions](docs/adr/), and [agent instructions](AGENTS.md).

## Development

Requirements and commands:

```sh
make verify
make verify-full
```

No production credentials belong in this repository. Start from `.env.example`.

## Project principles

1. Correctness and safety before throughput; optimize measured hot paths.
2. Modular monolith first; extract services only with evidence.
3. Money movement uses an immutable, idempotent, double-entry ledger.
4. Cross-module communication uses public APIs and domain events.
5. AI never autonomously decides money, safety, identity, or dispute outcomes.
6. Market-specific behavior is configuration plus adapters, not scattered conditionals.
7. A feature is complete only when its acceptance evidence and required checks pass.

## License

MIT. See [LICENSE](LICENSE).
