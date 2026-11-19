# ADR 0003: Initial Technology Stack

- Status: Accepted
- Date: 2026-11-19

## Context
The platform prioritizes runtime performance, predictable resource use, low infrastructure cost, and maintainable automated delivery. The initial proposal listed overlapping alternatives (Rust/Go and multiple queue/cache choices) without selecting a coherent baseline.

## Decision
- Go for the transactional API and domain core.
- TypeScript with pnpm for web applications and repository tooling.
- Next.js for customer web; React + Vite for admin.
- Flutter for iOS/Android clients.
- Python only for isolated AI/data workers.
- PostgreSQL + PostGIS as the system of record.
- NATS JetStream for asynchronous messaging when needed; Valkey for cache/ephemeral coordination when justified.
- OpenAPI and JSON Schema for external and event contracts.
- Docker Compose for local infrastructure.

## Consequences
The initial runtime and toolchain are intentionally narrow. Rust, TigerBeetle, a dedicated search engine, Kubernetes, and additional databases are deferred until benchmarks or product requirements justify their cost.

## Alternatives considered
- Rust and Go together: deferred because two compiled backend ecosystems are unnecessary initially.
- Node/Bun as core API: viable, but Go is selected for the transactional service's runtime footprint and concurrency profile.
- Python as core API: deferred to keep AI dependencies out of the latency-sensitive transactional runtime.
- Redis/Dragonfly: Valkey is the default cache candidate; no cache is mandatory at the start.
