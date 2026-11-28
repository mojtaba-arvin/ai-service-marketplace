# Evidence-gated pull request review and merge

## Authority

The repository does not require a second human reviewer or a QA operator by default. An implementer who is authorized to act for the project maintainer may inspect the complete PR diff and merge that PR after every applicable gate passes.

GitHub does not allow a pull-request author to submit a formal approval of their own PR. Do not simulate, forge, or work around such an approval. The current `main` ruleset requires zero approving reviews, so an author approval is neither possible nor necessary. Self-review means a documented technical review by the implementer, not a GitHub APPROVE review event.

## Mandatory merge gates

Before merging, the implementer must verify all of the following against the exact current PR head and current base:

1. All required CI, security, dependency, CodeQL and repository guard checks are successful.
2. Required review threads are resolved; every actionable finding is fixed or explicitly documented as non-blocking with rationale.
3. The PR is not draft, the diff is within the linked issue scope, and the base branch has not introduced unreviewed conflicts.
4. The implementer has reviewed the full diff, tests, authorization boundaries, failure/retry/concurrency behavior, privacy/security impact, migrations/rollback and documentation.
5. The selected merge method is allowed by the active ruleset. Never force-push, bypass a ruleset, dismiss a required check, or merge a stale head.
6. The PR description records exact test commands, outcomes, CI run links, limitations and any skipped checks. A skipped check is not a pass.

If a check becomes stale or the head changes, re-run/re-check the affected gates before merging.

## Risk-based additional evidence

Use additional automated evidence proportional to risk; do not substitute a different person's identity for evidence:

- Financial, ledger, payout/refund, authorization, tenant-isolation, privacy/evidence, security, migration and concurrency changes require targeted negative-path, race/failure-injection, replay/idempotency and recovery tests as applicable.
- AI changes affecting consequential workflows require deterministic policy enforcement and versioned adversarial evaluation. AI remains advisory where the product policy says so.
- Production deployment, real provider activation, legal/regulatory decisions and external consumer compatibility require the actual adopter/provider/legal/consumer evidence. A second repository reviewer cannot manufacture or waive that evidence.
- If a requirement is genuinely underspecified in a security, financial, legal or destructive-migration decision, use the stop conditions in `AGENTS.md`. Ordinary engineering uncertainty and fixable CI failures are not reasons to wait for a human reviewer.

## Feature acceptance is separate

A merged PR is not, by itself, feature acceptance. The authorized maintainer records feature acceptance only after reviewing the implementation, relevant automated tests/evaluations, exact-head required checks, operational evidence where applicable, known limitations and the feature's acceptance criteria. The maintainer may be the implementation author. Never set `passes: true` or `verified_by` merely because a PR merged or CI is green.

## Audit record

For each merge, retain the PR URL, issue, merge commit SHA, exact head SHA, required workflow run URLs, test commands/results, self-review summary, unresolved limitations and the reason any optional check was skipped.