# ADR 0007 — Scoped operator permissions

- **Status:** Accepted
- **Date:** 2026-11-25
- **Scope:** M9 / ADMIN-001

## Context

The current `Authorize` policy grants a fixed set of sensitive actions to every actor with `RoleOperator` after tenant equality is checked. Operator checks are distributed across Store methods; some entry points check the role directly. `Actor` is also used in many positional composite literals, so adding permission state to it would be a broad and fragile refactor.

The initial operator membership is provisioned by the deployment-configured `BOOTSTRAP_OPERATOR_PHONE`. That configuration establishes identity and tenant membership, but it is not an action-level authorization model.

## Decision

1. **Role is not authority.** `RoleOperator` establishes eligibility to be considered for operator access only. It grants no sensitive action by itself. All sensitive operator actions require an explicit persisted grant for the exact `(identity_id, tenant_id, action)` tuple.
2. **Keep Actor transport-neutral.** Do not add permission lists, grant flags, or database state to `Actor`. Authorization is resolved by the owning Store/application boundary.
3. **Enforce at the mutation/read boundary.** Each operator-sensitive Store operation must check the grant and the current operator membership in the same PostgreSQL transaction as the protected operation. A route-only check is insufficient. A permission check failure, missing row, malformed action, missing membership, or cross-tenant mismatch denies access.
4. **Use an allowlisted action vocabulary.** Grants may be created only for declared sensitive `Action` constants. Unknown or future action strings are not implicitly granted.
5. **Separate mutable current state from immutable evidence.** Current grants are represented by tenant- and identity-scoped rows with one active grant per action. Grant and revoke operations append immutable audit events containing actor, target, tenant, action, reason, before/after state and timestamp in the same transaction. Revocation changes current grant state; audit rows cannot be updated or deleted by the application or database trigger.
6. **No self-grant or self-escalation.** Runtime grant/revoke authority requires a separate explicit `operator:permissions:manage` grant. The actor cannot grant, revoke, or otherwise elevate their own permissions. The target must have an existing operator membership in the same tenant. No public self-service grant endpoint is introduced as part of ADMIN-001.
7. **One-time deployment bootstrap is the trust root.** A dedicated, documented one-shot provisioning command, run by an authorized deployment operator using the deployment's privileged database connection, creates the initial action grants for an explicitly selected existing operator identity. It records a `system_bootstrap` audit principal, deployment reason and selected action set, and is guarded against repeat execution for that tenant. The bootstrap command is not exposed over HTTP and does not run on OTP request/login. After bootstrap, all further grants/revocations use the persisted `operator:permissions:manage` authority. The normal `BOOTSTRAP_OPERATOR_PHONE` setting continues to establish membership only; it never implies action grants.
8. **No grandfathering.** Existing operator access is not silently migrated into broad grants. Deployments must run the one-time bootstrap with a deliberate least-privilege action set before operator workflows become available under the new policy. Until then, sensitive operator actions fail closed.

## Consequences

- The pure policy function remains useful for role/ownership checks, but must not independently authorize sensitive operator actions. Store-level permission checks become the authoritative boundary.
- All direct `RoleOperator` gates and all calls to `Authorize` for operator actions must be inventoried and migrated. Tests that currently assume role-only access must create explicit grants through the same provisioning/test fixture path.
- PostgreSQL integration tests must cover absent grants, action isolation, tenant isolation, missing operator membership, revoked grants, self-grant denial, immutable audit events, bootstrap replay denial, transaction rollback, and concurrent revocation versus a protected operation.
- Deployment documentation must describe the bootstrap command and the least-privilege initial grant set. No production secret or credential is committed.
- `feature_list.json` remains `passes=false` until implementation, exact-head checks and independent acceptance are complete.

## Implementation order

1. Add grant/current-state and append-only audit schema plus the one-time bootstrap marker.
2. Implement transaction-aware Store authorization and provisioning primitives.
3. Migrate every operator-sensitive read/write path and remove role-only authorization.
4. Update integration tests and security documentation; prove revoke/mutation concurrency behavior.
5. Add deployment bootstrap instructions and operational evidence.
