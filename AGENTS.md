# Agent Guide

## Mission
Build a production-grade, open-source, multi-market home-services marketplace. Read `docs/vision/business-plan.md` and `docs/vision/tech-plan.md` before making architectural changes.

## Start here
1. Read this file and the relevant module README/spec.
2. Read applicable ADRs in `docs/adr/`.
3. Read `docs/progress.md` and the linked GitHub issue.
4. Work only on the issue's stated scope and branch.

## Repository map
- `apps/web`: customer-facing SEO-first web application.
- `apps/admin`: internal operations console.
- `apps/mobile-customer`, `apps/mobile-pro`: Flutter clients.
- `apps/api`: Go modular-monolith API.
- `services/ai-worker`: isolated Python AI workloads.
- `packages/contracts`: OpenAPI, JSON Schema, generated API contracts.
- `packages/domain-*`: shared, explicitly public domain contracts only.
- `docs/vision`, `docs/adr`, `docs/specs`, `docs/playbooks`: project truth.
- `evals`: deterministic AI evaluation datasets and runners.
- `infra`: local and deployment infrastructure.
- `scripts`: repository checks and automation.

## Non-negotiable engineering rules
- Keep module internals private. Cross-module access must use a documented public API or event contract.
- Do not introduce microservices, caches, queues, or databases without a measured or explicit requirement.
- Store money as integer minor units with an explicit currency. Never use floating point for money.
- Ledger entries are append-only. Correct mistakes with compensating entries, never mutation.
- Make external side effects idempotent; use transactional outbox for reliable event publication.
- Enforce authorization on the server and default to least privilege.
- Never log secrets, authentication tokens, raw identity documents, or unredacted sensitive payloads.
- AI output affecting money, safety, identity, or reputation requires deterministic policy checks and/or human approval.
- Every adapter must have a local fake and contract tests; CI must not require live third-party credentials.
- Add tests for behavior and failure paths. Do not weaken tests to make an implementation pass.

## Agent permissions
- Implementers: change only issue-scoped implementation files. Do not edit `.github/**`, `tests/spec/**`, security policy, or feature status fields.
- Spec writers: own acceptance criteria and specification tests; do not implement production behavior.
- Reviewers: review only; do not modify code.
- Only an independently executed verification/evaluation workflow may mark a feature as passing.
- Do not self-approve or merge a pull request.

## Workflow
- Use short-lived issue-based branches: `work/<issue>-<slug>`. The number is the linked GitHub Issue, and the slug describes the change; never encode the contributor or automation identity in the branch name.
- Use Conventional Commits and link each PR to its issue.
- Keep PRs small, focused, and normally below 400 changed lines.
- Run `make verify`; run `make verify-full` when infrastructure or integration behavior changes.
- Report exact commands and outcomes. Never claim a check passed unless it was actually run.

## Stop conditions
Stop and label the issue `needs-human` when requirements conflict, a security/financial decision is underspecified, a migration is destructive, or the same verification failure persists after three focused attempts. Record the blocker and a concrete decision request.

## Definition of done
Acceptance criteria are met; tests cover success and failure paths; docs/contracts are current; security and module boundaries hold; required CI is green; no secrets are present; the PR explains evidence and known limitations.

## Playbooks
Use the applicable guides in `docs/playbooks/`: endpoint, migration, adapter, spec/test, and blocker workflows.
