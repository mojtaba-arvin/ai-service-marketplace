# One-time operator permission bootstrap

ADMIN-001 deliberately separates an operator's eligibility membership from the actions that identity may perform. Existing operator memberships receive **no implicit grants**.

This command is a deployment-time trust-root operation, not an HTTP endpoint and not part of OTP login. Run it only after migration 031 is applied, after verifying the target identity out of band, and using a privileged deployment database credential. The bootstrap marker is unique per tenant; a second run for the same tenant is rejected even if the requested manifest is identical.

Build/run from the repository root:

```sh
cd apps/api
DATABASE_URL='postgres://…' \
BOOTSTRAP_TENANT_ID='market-om' \
BOOTSTRAP_OPERATOR_IDENTITY_ID='verified-phone-identity-id' \
BOOTSTRAP_OPERATOR_ACTIONS='order:read,order:assign,provider:verify' \
BOOTSTRAP_REASON='Initial least-privilege operator grant approved by deployment owner' \
go run ./cmd/operator-permissions-bootstrap
```

The identity must already have an `operator` row in `identity_memberships` for exactly the supplied tenant. Actions are allowlisted, sorted for a stable manifest hash, and granted in one serializable transaction with a `system_bootstrap` audit event for every grant. A failed transaction leaves no marker or partial grants.

Start with the smallest action set needed by the initial operator. Do not include `operator:permissions:manage` unless a separately approved permission administrator is intentionally being provisioned. Never run this command from a web request, OTP flow, CI job, or with a developer's routine application credential.

This command only provisions grants. Runtime enforcement at every operator Store boundary and the human grant/revoke workflow are separate ADMIN-001 implementation gates; do not treat this bootstrap foundation as completion of ADMIN-001.
