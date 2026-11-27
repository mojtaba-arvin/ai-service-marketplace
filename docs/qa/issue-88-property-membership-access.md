# Issue #88 — property membership implementation evidence

Status: implementation integrated on `main`; feature acceptance remains pending.

## Reconciliation

The reserved branch originally carried the property-delegation implementation submitted in PR #105. That PR was closed without merge. The implementation was subsequently reconciled into the current `main` architecture through the organization/property lifecycle work recorded in PRs #106 and #108; the current source of truth is the implementation on `main`, not the superseded branch-only `property_membership.go` copy.

This branch is reconciled to the current `main` tree without carrying a second implementation of the same routes or store operations.

## Data and authorization model

- A tenant is the security/data boundary. An organization is a business grouping inside one tenant; neither membership nor a shared user identity grants access across tenants or organizations.
- Organizations, organization memberships, properties, property delegations and audit events are tenant-scoped in migration `024_organizations_properties.sql`.
- Composite foreign keys bind organization/property/member/delegate/creator/audit-actor references to the same tenant and organization.
- Every read and mutation resolves the authenticated identity against the selected tenant and current active organization membership in PostgreSQL. `X-Tenant-ID` selects context only; it is not authorization evidence.
- Owners/admins/managers have the documented inherited property visibility. Members/viewers see only explicitly delegated active properties. Property delegation grants visibility only, not order creation or property-write authority.
- Invitation acceptance is identity-bound; revocation immediately removes organization access and revokes property delegations transactionally. Membership mutations preserve append-only audit history and transactional outbox events.
- Organization/property archive is soft and non-destructive. It retains source rows and does not delete orders, journals, entries, or historical audit records.

## Financial and order boundary

No organization balance, order attribution, consolidated billing write path, payment capture, settlement, refund execution or real-money movement is introduced here.

ADR 0006 remains the governing staged design: future organization statements must be rebuildable read models over explicitly attributed immutable ledger postings, partitioned by tenant, organization, property, currency and period. Legacy orders/journals stay unattributed absent explicit audited reconciliation. Property visibility is not the future `service_order.create` capability.

## Migration and rollback

Migration 024 is additive and retains organizations, properties, memberships, delegations and audit rows through soft archive/revocation. Rollback is application-first: disable organization/property routes and consumers, retain schema and historical rows, and do not drop populated references or mutate audit/ledger history. Any physical schema removal requires a separately reviewed migration after proving there are no dependent records. The future order/finance attribution rollout and rollback sequence is specified in ADR 0006; it is not enabled by this issue.

## PostgreSQL and exact-head evidence

Baseline main commit `efd320e45da6438eca91dae0ccd408324979f079` completed CI run [46041052899](https://github.com/mojtaba-arvin/ai-service-marketplace/actions/runs/46041052899). Its `verify` job completed successfully and the workflow provisions PostgreSQL 17, sets `TEST_DATABASE_URL`, and runs `go test -race ./...`. The PostgreSQL suite includes:

- `TestOrganizationPropertySchemaEnforcesTenantBoundariesPostgres`
- `TestOrganizationMembershipLifecycleAndInvitationInboxPostgres`
- `TestOrganizationPropertyLifecycleAndDelegationsPostgres`

The suite also runs the tenant-scoped order/API integration coverage. This is repository CI evidence, not proof that the broader #57 release/tenant-isolation QA gate is accepted. The #57 issue remains open; its broader cross-resource/release evidence must not be inferred from this B2B suite.

The current PR's exact head must independently pass required CI, including the PostgreSQL-backed race test job, before this evidence is considered current. No feature acceptance fields are changed by this document.
