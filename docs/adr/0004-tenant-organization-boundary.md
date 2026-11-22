# ADR 0004: Keep Tenant and Organization as Separate Concepts

- Status: Accepted
- Date: 2026-11-22

## Context

The platform needs strict data isolation for customers, professionals, operators and support agents, while future B2B workflows may let one organization manage several properties, members or service locations. Treating an organization as the security tenant would incorrectly assume a one-to-one relationship and could either fragment legitimate organizations or broaden access across unrelated data.

## Decision

- A **tenant** is a security and data-isolation boundary. Every authenticated operation resolves an explicit identity + tenant + role membership.
- An **organization** is a business grouping inside a tenant-scoped authorization model. It is not an authorization bypass and does not automatically grant access to every record in the tenant.
- Organization memberships and property delegations are explicit, role-scoped, revocable and auditable.
- Orders, support cases, evidence and financial records retain their tenant ownership independently of organization membership.
- Consolidated billing is a read model over immutable ledger postings; it is not a mutable organization balance.
- Cross-tenant organization/property references must be rejected by database constraints and service authorization.

## Consequences

- B2B membership and property models require explicit authorization and revocation semantics.
- A user may belong to multiple organizations, and an organization may relate to multiple properties without changing the tenant boundary.
- Analytics and finance must preserve tenant attribution even when aggregating organization views.
- Migration `024_organizations_properties.sql` and the tenant-scoped organization/property API implement organization reads/writes, invitation acceptance, role changes and revocation. The authenticated invitation inbox exposes only pending invitations for the current identity and selected tenant. These operations do not make the organization a tenant or grant implicit access to tenant records.
- Organization membership mutations serialize on the organization row; final-owner protection, property-delegation revocation, append-only audit and transactional outbox behavior are enforced in the service/database transaction. Property delegation mutations additionally lock the organization and property rows; only owner/admin/manager can manage active member/viewer delegations, with manager/viewer property roles. Automated CI is not independent feature QA; acceptance remains tracked in #88.

## Alternatives considered

- Organization equals tenant: rejected because business grouping and security isolation are different concerns.
- Global organization membership across tenants: rejected because it creates implicit cross-tenant access.
- Store organization IDs on every record before B2B scope is defined: deferred until the data ownership and migration contract is reviewed.
