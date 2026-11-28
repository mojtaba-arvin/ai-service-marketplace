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
- Implementers: change only issue-scoped implementation files. Do not edit `.github/**`, `tests/spec/**`, security policy, or feature status fields unless a maintainer-authorized policy issue explicitly scopes those paths and the work-item guard grants a narrow, issue-specific exception.
- Spec writers: own acceptance criteria and specification tests; do not implement production behavior.
- Reviewers: review only; do not modify code.
- Feature acceptance does not require an independent QA operator or external reviewer. The implementation agent must provide reproducible test and exact-head CI/security evidence; the project maintainer (including an authorized ChatGPT operator acting for the maintainer) may inspect that evidence and mark the feature accepted.
- Implementers may perform and record a complete self-review, then merge their own PR when authorized to act for the maintainer and all exact-head required checks, review-thread resolution, and repository rules are satisfied. GitHub does not allow a PR author to submit a formal approval of their own PR; this repository's main ruleset requires zero approvals, so no second-person approval is needed. Never bypass a required check, unresolved thread, or branch rule.

## Issue title contract
- Every Issue title must use `<type>(<scope>): <short, clear description>`.
- Use a lowercase Conventional Commit type: `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `perf`, or `revert`.
- Use a concise lowercase kebab-case scope naming the affected domain/component (for example `identity`, `events`, `api`, `deployment`).
- Write a specific, action-oriented description. Keep priority, milestone, feature IDs, command IDs, agent names, and workflow/run metadata in labels, milestones, or the Issue body—not in the title.
- Do not use bracketed prefixes, standalone priority codes, `Recover:`, or agent/supervisor identity prefixes.
- For roadmap/milestone bookkeeping, use `chore(roadmap): <description>`; for supervisor ledgers use `chore(supervisor): <description>`.
- Examples: `feat(identity): add customer address book`, `fix(events): preserve additive event compatibility`, `test(ai): expand safety evaluation gates`, `docs(agents): clarify worker execution rules`.
- Preserve the Issue number and all body, labels, milestone, assignees, and relationships when renaming.

## Workflow
- Create or identify the GitHub Issue before creating a work branch. The Issue number is mandatory and must be embedded in the branch name.
- Canonical branch format: `<type>/issue-<number>-<slug>`.
- Allowed type prefixes: `feature`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `perf`, `revert`.
- The slug must be lowercase kebab-case, non-empty, and describe the Issue; do not include a contributor, agent, or automation identity.
- Examples: `feature/issue-123-new-login`, `fix/issue-124-token-refresh`, `docs/issue-125-api-authentication`.
- Do not create new `work/*` or `agent/*` branches. Those prefixes are legacy-only and may remain on pre-existing branches/PRs during transition; do not rename them as part of unrelated work.
- Use Conventional Commits and link each PR to its exact Issue with `Closes #<number>` or `Refs #<number>`.
- Keep PRs small, focused, and normally below 400 changed lines.
- Run `make verify`; run `make verify-full` when infrastructure or integration behavior changes.
- Report exact commands and outcomes. Never claim a check passed unless it was actually run.

## Stop conditions
Stop and label the issue `needs-human` when requirements conflict, a security/financial decision is underspecified, a migration is destructive, or the same verification failure persists after three focused attempts. Record the blocker and a concrete decision request.

## Definition of done
Acceptance criteria are met; tests cover success and failure paths; docs/contracts are current; security and module boundaries hold; required CI is green; no secrets are present; the PR explains evidence and known limitations.

## Playbooks
Use the applicable guides in `docs/playbooks/`: endpoint, migration, adapter, spec/test, blocker, and [evidence-gated PR review/merge](docs/playbooks/evidence-gated-pr-merge.md) workflows.

## Project acceptance policy

There is no separate or independent QA role in this project. Acceptance is performed by the project maintainer using implementation review, automated tests, exact-head required CI/security checks, and recorded evidence. Do not leave completed work blocked waiting for an independent operator, independent QA run, or external sign-off. Do not claim acceptance when required evidence is missing or failing. `feature_list.json` status fields may be updated by the authorized maintainer after this evidence review.
