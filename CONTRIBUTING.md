# Contributing

Thank you for helping improve this open-source marketplace platform. Contributions should make the project more correct, secure, portable, observable and easier to extend—not merely add surface area.

## Before opening a pull request

1. Read [AGENTS.md](AGENTS.md), the relevant [module specification](docs/specs/README.md), applicable [ADRs](docs/adr/), and the linked issue.
2. For non-trivial work, open or select a focused issue with acceptance criteria before implementation.
3. Work on a short-lived issue-based branch using `<type>/issue-<number>-<slug>`, where the type is one of `feature`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `perf`, or `revert`; the slug is lowercase kebab-case.
4. Keep changes focused. Avoid unrelated formatting, dependency churn and premature service/infrastructure extraction.

## Issue title convention

Every GitHub Issue title must follow this exact format:

```text
<type>(<scope>): <short, clear description>
```

- `type` must be a lowercase Conventional Commit type: `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `perf`, or `revert`.
- `scope` must be a concise lowercase kebab-case domain/component, such as `identity`, `events`, `api`, or `deployment`.
- The description must state the work clearly and actionably.
- Keep priority, milestone, feature/command IDs, agent names, and run metadata in labels, milestones, or the Issue body—not the title.
- Do not use bracketed prefixes, standalone priority codes, `Recover:`, or agent/supervisor identity prefixes.
- Use `chore(roadmap): <description>` for milestone bookkeeping and `chore(supervisor): <description>` for supervisor run records.

Examples:

- `feat(identity): add customer address book`
- `fix(events): preserve additive event compatibility`
- `test(ai): expand safety evaluation gates`
- `docs(agents): clarify worker execution rules`

When correcting an existing title, preserve the Issue number, body, labels, milestone, assignees, and all relationships.

## Development and verification

Prerequisites:
- Go version declared by CI (currently Go 1.25).
- Python 3.12 for repository checks.
- Docker Compose for full infrastructure/integration rehearsal.
- Node.js/npm only when changing the web application.

Run the offline-friendly verification suite:

```sh
make verify
```

For changes involving database, Compose, outbox or end-to-end behavior, also run:

```sh
make verify-full
```

The full command starts local infrastructure. Stop it after testing with `docker compose down` when it is no longer needed. PostgreSQL integration tests use `TEST_DATABASE_URL`; do not point tests at production or a database containing personal data.

CI is authoritative for required repository checks. Report commands actually run and distinguish skipped tests (for example, tests skipped because `TEST_DATABASE_URL` is absent) from successful tests.

## Architecture and contracts

- Keep the transactional core a modular monolith unless measurements justify extraction.
- Respect module data ownership; communicate through public application interfaces or versioned events.
- Update the owning module spec, API/event contract, migrations and tests together.
- Use integer minor units and explicit currency for money.
- Make external side effects idempotent; handle unknown outcomes explicitly.
- Add local fakes and contract tests for provider integrations; CI must not require live credentials.
- Do not add real payment, KYC, tax, messaging or AI-provider credentials to the repository.

## Security, privacy and safety

- Enforce authorization server-side; tenant isolation is a hard boundary.
- Never log secrets, access tokens, raw identity documents or unredacted sensitive content.
- Do not include real customer data in fixtures, golden datasets, screenshots or test output.
- AI output is advisory only for consequential workflows; deterministic policy and human approval remain authoritative.
- Never weaken or delete tests to make a change pass.

## Pull request expectations

- Use a Conventional Commit-style title.
- Link the issue and state scope/out-of-scope items.
- Include tests for success, validation, authorization and failure paths; add concurrency tests when state can race.
- Document migration and rollback behavior.
- Explain security/privacy impact and known limitations.
- Do not self-attest feature acceptance. The project maintainer may set `passes` and `verified_by` after reviewing implementation, tests, and exact-head CI/security evidence; no separate independent QA operator is required.

## Review process

The implementer may review the complete diff and merge their own PR when authorized to act for the maintainer, all exact-head required checks pass, required review threads are resolved, and repository rules permit the merge. Follow the [evidence-gated PR review and merge playbook](docs/playbooks/evidence-gated-pr-merge.md). GitHub does not allow a PR author to submit a formal approval of their own PR; this repository requires zero approvals, so a separate reviewer is not needed for merge. Never bypass branch protection or required checks. Small, independently reviewable PRs are preferred.

## Feature acceptance

The project does not require a separate QA operator, reviewer identity, or external acceptance sign-off by default. The authorized maintainer may be the implementation author and may accept a feature after reviewing its implementation, relevant automated tests/evaluations, exact-head required checks, and documented limitations. Risk-specific automated evidence and genuine adopter/legal/provider decisions remain required where applicable. A PR merge alone never marks a feature accepted. This does not waive GitHub branch protection or required PR checks.
