# Support case lifecycle

Cases are tenant-scoped and private to their requester and same-tenant support agents. The caller supplies an explicit SLA due timestamp; this module does not invent service-level policy.

Support assignments require an active same-tenant support-agent record. Status transitions are guarded and each create, assignment, and transition writes an append-only before/after audit event in the same transaction.

The case body is private content. Do not expose it through cross-tenant search or unauthenticated routes.
