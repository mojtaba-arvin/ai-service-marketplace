# Contributing

Thank you for helping improve this open-source marketplace platform. Contributions should make the project more correct, secure, portable, observable and easier to extend—not merely add surface area.

## Before opening a pull request

1. Read [AGENTS.md](AGENTS.md), the relevant [module specification](docs/specs/README.md), applicable [ADRs](docs/adr/), and the linked issue.
2. For non-trivial work, open or select a focused issue with acceptance criteria before implementation.
3. Work on a short-lived branch using the repository convention `agent/<issue>-<slug>`.
4. Keep changes focused. Avoid unrelated formatting, dependency churn and premature service/infrastructure extraction.

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
- Do not mark feature inventory items as passing. Only independent verification may set `passes` and `verified_by`.

## Review process

A maintainer reviews correctness, security, architecture, tests, compatibility and documentation. Required CI checks must pass before merge. Do not self-approve or bypass branch protection. Small, independently reviewable PRs are preferred.
