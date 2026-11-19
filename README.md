# AI Service Marketplace

An open-source, multi-market home-services marketplace connecting customers with vetted independent professionals. The platform is designed around managed service quality, trusted transactions, recurring services, and auditable operations.

## Project status

The repository is in **Foundation / Session 0A**. This phase establishes architecture decisions, agent instructions, documentation conventions, and a verifiable development harness. Product capabilities are not yet implemented.

## Architecture at a glance

- **Core API:** Go, with explicit modular-monolith boundaries.
- **Customer web:** Next.js + TypeScript (SEO-first).
- **Operations console:** React + Vite + TypeScript.
- **Mobile clients:** Flutter (customer and professional applications).
- **AI workloads:** isolated Python workers behind an internal AI gateway.
- **Data:** PostgreSQL + PostGIS; pgvector only for use cases that justify vector retrieval.
- **Async and realtime infrastructure:** transactional outbox, NATS JetStream, and Valkey where caching or ephemeral coordination is justified.
- **Contracts:** OpenAPI and JSON Schema as versioned interface contracts.

See [the business vision](docs/vision/business-plan.md), [technical plan](docs/vision/tech-plan.md), [architecture decisions](docs/adr/), and [agent instructions](AGENTS.md).

## Development

Requirements and commands will be documented as the toolchain is introduced. The intended entry points are:

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

The repository license is not yet selected. See the open decision in the project backlog before reusing this software.
