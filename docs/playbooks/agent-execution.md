# Agent Execution and Blocker Discipline

This playbook applies to implementation workers, shared-integration owners, maintainers, and supervisors. It supplements module-specific guidance and never overrides repository security, review, migration, privacy, or release policy. The repository's `AGENTS.md` is authoritative when guidance conflicts.

## 1. Track dispositions per objective

A command or issue may contain several independently runnable acceptance objectives. Record one disposition for each objective:

- `PASS`: implemented or already implemented, with direct, reproducible evidence.
- `FAIL`: attempted and failed; include the failing behavior, command, test, or CI evidence.
- `WAITING_DEPENDENCY`: a named prerequisite, shared integration artifact, or upstream evidence is not ready. Record the exact dependency, evidence required, owner, and next action that makes the objective runnable.
- `BLOCKED`: an authoritative human, legal, provider, privacy, safety, financial, permission, or policy decision prevents this objective. Cite the controlling repository policy or authoritative source and ask one concrete decision.
- `NOT-RUN`: not attempted. Explain why, whether it remains runnable, and what would make it runnable.

A dependency wait is not a hard blocker. A failed check is not automatically a policy blocker. Do not promote one objective's disposition to the whole command. Stop only objectives that actually depend on the missing decision or artifact. Continue every unrelated objective that can safely proceed. A session is wholly waiting or blocked only when no remaining objective is safely runnable.

## 2. Partition shared integration from module-private work

Respect the repository's documented module boundaries and the ownership rules for shared paths:

- Domain workers own module-private implementation, tests, adapters, client code, and documentation within their issue scope.
- The designated shared-integration owner owns changes to shared contracts, schemas, migrations, cross-module registration, and shared store/domain wiring, as assigned by repository policy and the relevant issue.
- Infrastructure and workflow changes remain with their designated repository owners.
- Only the repository-authorized maintainer may update feature acceptance fields, as specified in `AGENTS.md`.

When an objective needs a shared change, split the work into:
1. module-private implementation and tests that can proceed against an existing public boundary;
2. a concrete integration proposal for the designated owner; and
3. final wiring or end-to-end verification that genuinely requires the shared change.

Continue part 1 whenever safe. Submit part 2 with affected objectives/features, proposed shape, invariants, authorization and tenant rules, migration/rollback considerations, tests, compatibility impact, and exact files. Mark only part 3 as `WAITING_DEPENDENCY`. Do not modify protected shared paths or bypass ownership to avoid waiting.

A missing schema, migration, endpoint registration, or shared store API is an integration dependency by default—not automatically a legal, security, or product blocker. Treat it as a hard blocker only when an authoritative requirement or existing contract makes even the module-private work unsafe.

## 3. Keep readiness and acceptance milestones distinct

Use these milestones without conflating them:

1. **Implementation readiness:** the assigned module-private behavior is implemented or reconciled against current `main`; each in-scope criterion maps to source and reproducible tests/evidence; remaining shared changes and dependencies are explicit.
2. **Integration readiness:** all required shared contracts and wiring are merged into current `main`, compatible with the module implementation, and verified at the relevant exact commit.
3. **Maintainer acceptance:** the project maintainer reviews the criteria, implementation, automated test/evaluation results, exact-head required CI/security checks, and recorded evidence, then makes the acceptance decision under `AGENTS.md`.

Implementation workers must not wait for downstream integration or maintainer review to publish truthful implementation-readiness evidence. Conversely, do not claim integration readiness while required shared integration remains pending, or maintainer acceptance before the maintainer has made that decision.

Automated tests, security checks, and exact-head CI are evidence for the maintainer's decision; they are not a claim that a separate independent QA operator or external sign-off exists or is required. Do not invent a separate acceptance workflow or change feature status fields outside the authority defined in `AGENTS.md`.

## 4. Reconcile existing implementation instead of stopping

When relevant code or tests already exist on `main`:

1. Map every acceptance criterion to exact source, tests, API/client behavior, documentation, and operational evidence.
2. Run or locate current verification. Distinguish observed CI from local commands, and skipped tests from passes.
3. Fix only concrete gaps, missing failure paths, security defects, client integration, or documentation within the assigned scope.
4. If implementation criteria are satisfied, publish implementation-readiness evidence with exact SHAs and test/CI references. Do not duplicate code or claim maintainer acceptance.
5. Keep missing shared integration as a dependency only for the objectives that require it; keep maintainer review as its own milestone.

A list of existing files or merged PR links is not sufficient by itself. Trace criteria to behavior and evidence.

## 5. Name dependencies and break cycles narrowly

Dependencies must identify the exact prerequisite and evidence needed by the dependent objective. Prefer explicit readiness evidence for implementation and integration; do not use an ambiguous acceptance label to mean both.

Before execution, inspect the dependency graph for cycles. A cycle is an engineering-plan defect, not a reason to block unrelated work. Do not dispatch objectives in the cyclic component until the minimal circular dependency is corrected. Continue acyclic objectives whose dependencies and ownership are satisfied.

## 6. Make checkpoints actionable

Every checkpoint must include:

- command/session and objective identifiers;
- per-objective disposition and exact reason;
- exact dependency and evidence key when waiting;
- authoritative policy/source when blocked;
- work that continued despite another objective waiting or blocked;
- changed files, commit, PR, and exact head SHA;
- commands/checks run and their actual outcomes;
- next action and the condition that makes it runnable.

Do not write a generic `BLOCKED` report when `WAITING_DEPENDENCY` or another objective-level disposition is correct. Do not claim a session is active merely because a command prompt was emitted, or a worker was launched unless the runtime actually launched it.

## 7. Preserve repository safety and ownership

This playbook does not permit direct pushes to `main`, self-approval, unauthorized self-merge, branch-protection bypass, unauthorized migrations, invented legal/provider/retention policy, weakened tests, or acceptance claims based on implementation or CI alone. Enforce each real gate for the exact action it governs; unrelated safe work continues.
