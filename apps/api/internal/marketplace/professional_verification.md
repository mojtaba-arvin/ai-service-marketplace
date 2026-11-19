# Professional verification cases

Professionals submit a private object-storage reference; evidence bytes are not stored in the marketplace database. The provider's tenant and identity are checked before case creation, and only one open case is allowed.

Same-tenant operators may approve or reject a submitted case, but cannot review their own case. Decisions require a reason and record reviewer identity and timestamps. Approval updates the provider verification fields used by dispatch eligibility; it does not activate the provider.

The subject can read case status but never receives the private evidence pointer. Same-tenant operators can retrieve it. The event log is append-only.
