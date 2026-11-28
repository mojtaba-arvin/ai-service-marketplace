# ADMIN-001 runtime authorization — Store-path inventory

## Invariant

Every operator-sensitive Store operation resolves the authenticated actor's current
operator membership and exact persisted `(identity_id, tenant_id, action)` grant
inside the same PostgreSQL transaction as its protected read or mutation. The
transaction retains the authorization row locks until the protected operation
commits or rolls back. A route-only check, a prior committed authorization check,
or an `Actor` carrying client-supplied tenant/permission state is not authority.

`Actor` remains transport-neutral. HTTP handlers derive its subject, role and
tenant from the authenticated server-side session/token. Request bodies and
query parameters must not supply or override operator identity or tenant.

## Dispatch order read

`Store.FindDispatchCandidatesForOrder` is an order-scoped protected read:

1. Begin one PostgreSQL transaction.
2. For an operator, call `authorizeOperatorTx` for `order:assign` in that
   transaction. Its membership and active-grant `FOR SHARE` locks serialize
   the complete read against membership/grant revocation.
3. Load the order using both its ID and the authenticated actor tenant. Unknown,
   unassigned legacy, and foreign-tenant order IDs return the same
   `ErrOrderNotFound` result.
4. Validate customer ownership (customer path), current `requested` state,
   scheduled time, immutable price-version reference and required location.
5. Resolve the category from the order's price version and query provider
   eligibility/availability using the same transaction and authenticated tenant.
6. Commit only after the candidate query has been fully consumed. Every error
   path rolls back and releases the authorization locks.

The customer-owned path keeps its existing `ActionOrderRead` ownership check.
The general category/location candidate search keeps its existing
`ActionOrderCreate` customer policy and uses the same transaction-aware
candidate query helper.

## ADMIN-001 Store-path inventory

The merged runtime enforcement in PR #213 places persisted-grant checks at the
Store boundary for the following operator-sensitive areas:

- Order, provider and analytics reads.
- Market and catalog configuration.
- Service-form draft/publish operations.
- Provider verification, availability and booking.
- Dispatch candidate and offer operations.
- Cancellation-policy writes.
- Professional-verification review and evidence reads.
- Tenant-scoped permission grant/history/revocation operations.

The dispatch order lookup is the additional order-scoped read invariant
documented above. The generic `FindDispatchCandidates` path remains distinct
from the order-scoped path: it authorizes the caller for its supplied
category/location query, while `FindDispatchCandidatesForOrder` derives those
inputs from a tenant-scoped, currently requested order.

## Security and verification boundaries

- Role membership is eligibility, never action authorization.
- Unknown, missing, revoked, stale-membership and cross-tenant contexts fail
  closed.
- Grant/revoke audit rows remain append-only; this change adds no grant schema,
  bootstrap flow, API, migration or permission vocabulary.
- Step-up approval requirements remain owned by their existing policy and are
  not bypassed or weakened here.
- Implementation/merge does not set ADMIN-001 `passes=true`. Acceptance still
  requires maintainer review of the complete Store inventory, exact-head
  required CI/security evidence and deployment bootstrap evidence.
