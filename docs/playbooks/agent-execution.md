# Agent Execution and Blocker Discipline

This playbook applies to every implementation worker, integration owner, QA verifier, and supervisor. It supplements the module-specific playbooks; it does not override repository security, review, migration, or release policy.

## 1. Work at objective granularity

A command or issue may contain multiple acceptance objectives. Track each objective independently as:

- `PASS`: implemented or already implemented, with direct reproducible evidence.
- `FAIL`: attempted and failed; include the failing test, behavior, or CI evidence.
- `WAITING_DEPENDENCY`: a named prerequisite or integration artifact is missing. Name the exact dependency, evidence key, and the next action that makes it runnable.
- `BLOCKED`: an explicit human, legal, provider, privacy, safety, financial, permission, or policy decision prevents this objective. Cite the authoritative source and ask one concrete decision.
- `NOT-RUN`: not attempted; explain why and whether it is still runnable.

Do not convert one objective's state into the whole command's state. If any objective remains safely runnable, continue it and keep the session in progress. End a session as waiting on a dependency or blocked only when no remaining objective can safely proceed.

## 2. Shared integration is not a blanket implementation blocker

Respect ownership boundaries:

- Domain workers own module-private implementation, tests, adapters, client code, and documentation within their issue scope.
- CMD-1754 owns shared migrations, OpenAPI/JSON Schema, shared HTTP registration, and shared store/domain wiring.
- CMD-1755 owns CI, deployment, Docker, infrastructure, and workflow changes.
- CMD-1766 alone changes feature inventory `passes` and `verified_by`.

When an objective needs a shared change, split it into:
1. the module-private implementation and tests that can proceed against an existing public boundary;
2. the exact shared integration proposal for its designated owner;
3. any final wiring or end-to-end verification that waits for integration.

Continue part 1 whenever it is safe. Submit part 2 with feature IDs, proposed shape, invariants, authorization/tenant rules, migration and rollback considerations, tests, compatibility impact, and exact files. Mark only part 3 as `WAITING_DEPENDENCY`. Never edit a protected shared path to avoid waiting.

A missing schema, migration, endpoint registration, or shared store API is not automatically a legal, security, or product blocker. It is an integration dependency unless the existing contract makes even module-private work unsafe.

## 3. Distinguish implementation, integration, QA, and acceptance

These are separate milestones:

1. **Implementation readiness:** assigned behavior is implemented or reconciled against current main, criterion-level tests/evidence exist, and remaining shared changes are explicitly proposed or identified.
2. **Integration readiness:** required shared contracts and wiring are merged and compatible.
3. **Independent QA:** a verifier independently exercises the merged behavior and publishes criterion-level PASS/FAIL/NOT-VERIFIED evidence.
4. **Feature acceptance:** only the controlled acceptance workflow updates `feature_list.json`.

Implementation workers must not wait for downstream QA or risk review to publish implementation-readiness evidence. A downstream verifier must never be a prerequisite for the implementation evidence that starts that verifier. Preserve explicit target-side human approvals where the requirement itself says they must precede implementation or external effects.



Use the typed evidence markers consistently:

- `cmd_<id>_implementation_ready`: module-private implementation is complete; shared integration may still be pending.
- `cmd_<id>_integration_ready`: all required shared migrations/contracts/wiring are merged on current main.
- Legacy `cmd_<id>_acceptance_ready` aliases `integration_ready`; it must never be published while required shared integration is pending.
- `cmd_<id>_independent_qa_pass`: verifier-only evidence from CMD-1760–1765.
- CMD-1766 alone updates feature inventory acceptance fields.

A worker may publish `implementation_ready` before CMD-1754 finishes a shared proposal. It must wait to publish `integration_ready` / `acceptance_ready` until the proposal is merged and verified, or until it proves no shared integration is required. QA commands that need the integrated surface must depend on `integration_ready` / `acceptance_ready`, not `implementation_ready`.

## 4. Reconcile existing implementation instead of stopping

If code or tests already exist on `main`:

1. Map every acceptance criterion to exact source, test, API/client behavior, docs, and operational evidence.
2. Run or locate current verification; distinguish observed CI from local commands and distinguish skipped tests from passes.
3. Fix only concrete gaps, missing failure paths, security defects, client integration, or missing documentation within the assigned scope.
4. If all implementation criteria are satisfied, publish implementation-readiness evidence with exact SHAs and test/CI references. Do not duplicate code and do not call the feature independently accepted.
5. Keep missing independent QA as a downstream state, not an implementation blocker.

An inventory of existing files or merged PR links is not sufficient by itself; the criteria must be traced to behavior and evidence.

## 5. Dependency and cycle handling

Dependencies must name the exact evidence needed by the dependent objective. Prefer typed markers:

- `cmd_<id>_implementation_ready`
- `cmd_<id>_integration_ready`
- `cmd_<id>_independent_qa_pass`

Legacy `cmd_<id>_acceptance_ready` markers are interpreted as implementation readiness only, never as independent QA or feature acceptance.

Before execution, inspect the dependency graph for cycles. A cycle is an engineering-plan defect, not a reason to block unrelated work. Do not dispatch the cyclic component until the minimal circular evidence edge is corrected. Continue acyclic work whose dependencies and ownership are satisfied.

## 6. Required checkpoint quality

Every checkpoint must include:

- command and session ID;
- objective/feature IDs;
- disposition and exact reason;
- exact dependency/evidence key when waiting;
- authoritative policy/source when blocked;
- files/commit/PR/head SHA when changed;
- test commands and actual outcomes;
- the next action and the condition that makes it runnable.

Do not write a generic `BLOCKED` report when a more precise `WAITING_DEPENDENCY` or objective-level disposition is correct. Do not claim a session is active merely because a command prompt was emitted. Do not claim a worker was launched unless the runtime actually launched it.

## 7. Safety remains strict

This playbook does not permit direct pushes to `main`, self-approval, self-merge, branch-protection bypass, unauthorized migrations, invented legal/provider/retention policy, weakened tests, or feature acceptance claims from implementation/CI alone. A real gate remains enforced for the exact action it governs; unrelated work continues.
