# Shared-contract integration queue (Issue #232)

**Inventory timestamp:** 2026-11-28  
**Inspected main:** `efd320e45da6438eca91dae0ccd408324979f079`  
**Coordinator branch:** `feature/issue-232-integrate-cross-module-contracts`

This is a serialized proposal and ownership ledger. It does not declare any feature accepted, migration applied, PR mergeable, or external consumer compatible. Reconcile every PR against current main immediately before integration; branch heads and check results are mutable.

## 1. Ownership contract

Issue #232 is the sole coordinator for shared integration artifacts:

| Shared surface | Coordinator-owned paths | Worker rule |
|---|---|---|
| PostgreSQL migration registry/schema | `apps/api/migrations/**`, migration runner/test harness where needed | Domain workers submit migration proposals; no direct edits |
| HTTP public contract | `packages/contracts/openapi.v1.json`, shared JSON Schemas under `packages/contracts/**` and `schemas/**` | Workers submit DTO/route proposals; no direct edits |
| Event envelope/schema compatibility | `schemas/domain-event.schema.json`, event schemas under `packages/contracts/events/**` | Producer/consumer workers own only private code and tests; schema changes require coordinator review |
| Shared HTTP registration | `apps/api/internal/marketplace/http.go`, `mvp_http.go` | Workers provide handler/application interface proposals; no direct route registration |
| Shared store/domain integration | `apps/api/internal/marketplace/store.go` and shared domain/actor/registration files | Workers do not modify shared wiring/types |
| Cross-module coordination | this queue and feature-linked integration proposals | No feature acceptance flags or production operations |

Root `AGENTS.md` governs all paths. The current repository has no nested `AGENTS.md` files. Module-private ownership remains with the owning feature branch; the coordinator consumes only the documented public application boundary.

## 2. Current baseline and active PR inventory

Main is protected at `efd320e45da6438eca91dae0ccd408324979f079`. Main currently contains migrations 001–035; latest is `035_private_incident_evidence.sql`. There is no migration 036 on main.

Open PRs at inventory time:

| PR | Head / branch | Scope and relevant files | Integration disposition |
|---|---|---|---|
| #285 | `0fbfb478` docs/issue-224-align-market-code-contract | `apps/api/internal/marketconfig/README.md` | Non-shared docs; preserve and reconcile market-code wording |
| #284 | `e37abd16` feature/issue-262-additive-event-compatibility (Draft) | `order_history.go`, `scheduled_orders.go`, tests, `outbox/http_publisher.go`, tests, OpenAPI | Shared OpenAPI collision with #266/#249; keep Draft until consumer binding and #87 evidence |
| #283 | `912b28ba` chore/issue-282-commit-identity-policy (Draft) | `AGENTS.md`, `COMMIT_IDENTITY.md`, identity-check script | Workflow/policy change; independent of this integration queue |
| #276 | `050f9db7` fix/issue-275-migrate-cancellation-producers-to-canonical-tenant | cancellation code/tests and migration playbook | Blocked by additive compatibility and #87 external-consumer gate |
| #272 | `c0b6b7b5` feature/issue-271-add-external-identity-verification-and-linking | adapter and identity-module private code/tests | Keep private; depends on coordinator-owned nonce/link schema and routes |
| #270 | `7c52307e` feature/issue-269-add-customer-address-book-and-geocoding | adapter and identity-module private code/tests | Keep private; depends on coordinator-owned address/snapshot schema and routes |
| #266 | `ae21f1bd` feature/issue-263-emit-canonical-tenant-scoped-scheduled-order-events | analytics/projector regression, order-history/scheduled-order/outbox code/tests, OpenAPI | Overlaps #284 on the same event files and OpenAPI; consolidate once, never merge both copies independently |
| #265 | `a5c84b27` docs/issue-260-record-address-and-provider-integration-boundaries | adapter README and identity spec | Non-shared proposal documentation; reconcile with #270/#272 contracts |
| #264 | `05bac168` docs/issue-259-prevent-command-wide-false-blockers | execution/blocker playbooks | Independent docs |
| #258 | `b0577dd8` docs/issue-257-align-branch-contract | `AGENTS.md`, `CONTRIBUTING.md` | Workflow docs; preserve, review separately |
| #255 | `68e7ae74` feature/issue-228-secure-category-lifecycle-and-market-visibility | `catalog.go`, `catalog_localization.go`, tests, marketplace-core spec | Private catalog logic; behind main; shared route/OpenAPI integration is this issue |
| #252 | `9a7d157f` feature/issue-235-add-provider-neutral-credential-review-lifecycle | trust verification policy and professional verification private code/tests/spec | Separate trust proposal; no shared migration/API registration in this PR |
| #249 | `b7e329f7` feature/issue-217-add-separately-authorized-evidence-lifecycle-management | migration 036, OpenAPI, migrate/bootstrap wiring, evidence code/tests, ADR 0010, progress docs | Shared-path collision: migration 036 and OpenAPI overlap coordinator ownership. PR is dirty and based on stale main. Preserve unique work; obtain exact owner disposition and reconcile before any shared write/rebase/integration |

Historical #223 is closed and superseded by #249; it is not an active blocker and was not merged. Its original 036 proposal is preserved in #249. Closed #227 is test-only and does not allocate a migration. Earlier #110/#137 were superseded/recovered into current event work; inspect their closed/open state before relying on old notes.

## 3. Serialized integration queue

No later row may be integrated ahead of an unmet predecessor. Independent module-private work may continue in parallel only within its owned files.

### Q0 — Shared-path collision disposition: #249 (TRUST-002 lifecycle)

**Priority:** prerequisite before any shared migration/OpenAPI write.  
**Proposal:** #249 adds `036_incident_evidence_lifecycle.sql`, evidence lifecycle API and tests, permission bootstrap wiring, ADR 0010 update and OpenAPI additions.  
**Shared paths:** `apps/api/migrations/036_incident_evidence_lifecycle.sql`; `packages/contracts/openapi.v1.json`; `apps/api/cmd/migrate/main.go`; `apps/api/cmd/operator-permissions-bootstrap/main.go`; related permission/policy wiring; `docs/adr/0010-private-incident-evidence-lifecycle.md`; `docs/progress.md`.  
**Dependency:** current main already includes 001–035; #249 currently targets a stale base and reports dirty mergeability.  
**Decision:** do not allocate migration 036 to another feature or overwrite its proposal. Preserve all #249 commits. Rebase/resolve only after the owner has reconciled the exact current-main diff and every OpenAPI conflict.  
**Acceptance evidence:** exact-head migration up/down rehearsal on disposable PostgreSQL; idempotent migration runner; PostgreSQL authorization/tenant/audit/immutability tests; OpenAPI validation and backward-compatibility diff; required exact-head CI/security checks. Rollback cutoff must be documented. No production rollout or retention policy is implied.

### Q1 — CAT-001–003 catalog API integration: #228 / #255

**Module-private owner:** #255 owns `apps/api/internal/marketplace/catalog.go`, `catalog_localization.go`, their tests and `docs/specs/marketplace-core.md`. CAT-002's existing `providers.go` remains provider/eligibility-owned; do not move or duplicate it.  
**Coordinator-owned shared paths:** `apps/api/internal/marketplace/mvp_http.go` / route registration in `http.go`; `packages/contracts/openapi.v1.json`; migration registry only if a proven schema gap exists.  
**Existing persistence:** migrations 004 (catalog) and 008 (localization) are already on main. Do not create duplicate taxonomy/visibility/translation tables.  
**Proposal to integrate:** map each lifecycle action (create, move, enable, disable) to the existing catalog application boundary and the explicit platform catalog-management grant; map market/locale discovery to existing market visibility and translation reads; include ancestor-aware visibility, selected market+locale validation, bounded pagination and stable error semantics. No public provider write API or provider policy is inferred from this catalog proposal.  
**Dependencies:** #255's private implementation reconciled onto current main; exact auth actor/grant and marketconfig APIs; Q0 OpenAPI ownership clearance.  
**Acceptance evidence:** PostgreSQL positive/negative tests for authorization, cross-tenant denial, parent/child hierarchy races, cycles, disabled ancestors and market visibility; handler tests for all route methods and invalid market/locale; OpenAPI schema/operation coverage; old route compatibility; exact-head CI. Migration only if a concrete missing constraint is demonstrated with owner-approved proposal.

### Q2 — IDENT-008/009 address, snapshot, nonce and provider integration: #234 children #260/#269/#271/#273

**Module-private owners:** #269/#270 owns `apps/api/internal/marketplace/addresses.go`, `apps/api/internal/adapters/geocoding.go` and private tests. #271/#272 owns `addresses_identity.go`, `identity_token.go` and private tests. #260/#265 owns integration-boundary documentation. #273 is a recovered combined branch; preserve its unique commits but do not merge duplicate implementations on top of #270/#272. Reconcile exact unique diff against both replacement PRs first.  
**Coordinator-owned shared paths:** migration file(s) under `apps/api/migrations/**`; `packages/contracts/openapi.v1.json`; shared identity HTTP route registration in `http.go` / `mvp_http.go`; shared store/domain wiring and order linkage in `store.go` / order creation boundary.  
**Migration proposal (next available number after Q0 is accepted):** additive schema for customer saved addresses; immutable order service-address snapshots; nullable source-address FK from order using RESTRICT; unique external provider-subject links to an existing identity; persisted one-time provider nonce digests with expiry/consumption. Use text IDs consistent with current tables. Add owner/tenant/market indexes, coordinate range checks, one-default-per-identity/tenant/market uniqueness, provider-subject uniqueness and nonce replay constraints. Exact FK names and current order/identity column compatibility must be verified against main before authoring.  
**Order:** (1) resolve and lock authenticated identity/tenant/market context; (2) persist saved address; (3) atomically create immutable snapshot with order and optional RESTRICT source FK; (4) enforce snapshot visibility only to customer and assigned professional; (5) provider linking consumes nonce and creates unique link in one transaction; provider login resolves only existing link and issues normal opaque session.  
**API proposal:** customer address list/create/update/delete/default/geocode; provider link/login start/callback. Exact paths and DTOs must be aligned with existing route conventions before OpenAPI publication. Never expose tokens, assertions, nonce digests, raw provider claims or unredacted address data in logs/events/errors. No email-only linking.  
**Dependencies:** #269/#271 private diffs reviewed and current-base CI; #260 boundary decisions; Q0 migration/OpenAPI collision cleared; exact marketconfig capability contract. #273 branch unique changes preserved/dispositioned.  
**Acceptance evidence:** PostgreSQL tests for CRUD, customer/tenant/market isolation, default races, FK restrict, concurrent order/delete, snapshot immutability after saved-address edits, assigned/unassigned professional visibility, unique provider subject, nonce expiry/replay/atomic consumption and session issuance only from verified linked identities. Adapter tests remain deterministic/offline. OpenAPI/route auth tests and exact-head CI. No live credentials, provider activation, production geocoding or backfill.

### Q3 — Canonical event consumer and compatibility gate: #87 / #262 / #284

**Shared artifacts:** `schemas/domain-event.schema.json`; event schemas under `packages/contracts/events/**`; `packages/contracts/openapi.v1.json`; publisher/timeline integration points.  
**Current producers/consumers:** scheduled-order work #263/#266; cancellation work #275/#276; recovered compatibility #262/#284; in-repo analytics projector.  
**Compatibility design:** preserve historical `payload` byte/shape semantics for all legacy events. Add canonical envelope only as a distinct optional field where a consumer explicitly tolerates unknown fields; malformed/mixed legacy-version payloads fail closed. Validate event type, schema version, aggregate ID and tenant ID against persisted authorized aggregate context. Do not reinterpret a legacy business object's `version` as envelope version.  
**External-consumer gate:** repository currently documents a generic `OUTBOX_PUBLISH_URL` HTTPS sink but does not name concrete downstream consumers, owners or accepted schemas. Internal analytics/projector tests are not external-consumer evidence. Inventory each actual sink/client, version and owner; obtain per-consumer contract test or a named, approved versioned migration window with rollback and compatibility owner. Until that exists, do not enable a publisher shape change that could break an external sink.  
**Dependencies:** Q0 OpenAPI conflict resolution; consumer inventory and tests under #87; consolidate #284 and #266 overlapping order-history/scheduled-order/outbox/OpenAPI hunks exactly once. #276 depends on the same adapter plus #87.  
**Acceptance evidence:** schema validation at publisher/consumer boundary; positive and negative contract tests for every actual sink; tenant/aggregate binding; duplicate/replay/reorder behavior; malformed envelope, unsupported version, unknown optional field and legacy payload regression tests; privacy tests rejecting phone/token/address/evidence fields; exact-head CI. No event producer is unblocked solely by internal CI.

### Q4 — Cancellation producer integration: #275 / #276

**Private owner:** `apps/api/internal/marketplace/cancellation.go` and tests; `docs/playbooks/canonical-cancellation-event-migration.md`.  
**Shared dependency:** Q3 compatibility adapter and #87 external-consumer gate.  
**Acceptance evidence:** PostgreSQL atomicity of cancellation/order/booking release/outbox; idempotent replay emits exactly one event; canonical tenant derived from persisted authorized order/Actor; legacy business fields preserved; sink contract tests for every consumer. Until Q3 is satisfied, keep PR #276 unmerged.

### Q5 — Other shared proposals and conflicts

- #252/#235 TRUST-008: private trust policy proposal only. No migration or shared route until exact market policy, consent provenance and dispatch fail-closed behavior are specified. No invented provider/retention/legal criteria.
- #285/#224: market-code docs only; reconcile exact market identifier contract with current marketconfig before changing schemas.
- #249 is the only active PR with a new migration file at inventory time; do not allocate 037+ until Q0/Q1/Q2 proposal review proves schema necessity.
- #283/#258 change root agent/commit policy; review separately and do not let their branch changes overwrite this queue.
- No active PR is authorized to change `feature_list.json` acceptance fields through this coordinator.

## 4. Migration sequence and collision rules

1. Mainline migrations 001–035 are the only migrations currently present on main.
2. Reserve 036 for the preserved #249 evidence-lifecycle proposal; do not call it applied until merged and PostgreSQL migration CI passes.
3. Catalog Q1 should reuse 004/008 unless concrete schema inspection proves a missing constraint/table. No speculative migration.
4. Address/provider Q2 receives the next available migration number only after Q0 is integrated and the exact main schema is re-read. Its proposal must include address, immutable snapshot/order linkage, external identity link and nonce state with all constraints.
5. Event Q3 should be schema/compatibility work without rewriting historical outbox rows. Do not create a migration merely to transform legacy payloads; use additive/versioned compatibility and a reviewed migration window.
6. Q4 producer changes follow Q3; no destructive migration or production backfill.
7. For any future migration: one coordinator-owned PR at a time; additive expand/contract; estimate locks; verify constraints against existing data; document rollback cutoff; run supported PostgreSQL migration tests and `make verify-full`. Never use destructive rollback after dependent data exists.

## 5. ADR invariants and global acceptance gates

- **ADR 0004:** tenant is the security boundary; organization is not a tenant or authorization shortcut. Cross-tenant references fail at database and service boundaries.
- **ADR 0005:** canonical v1 event identity/type/version/time/tenant/aggregate/data; tenant derives from authorized aggregate context; minimize data; no historical payload rewrite; idempotent at-least-once consumers.
- **ADR 0006:** B2B order attribution is explicit and tenant-preserving. Never infer tenant/organization/property/order attribution from identity, amount, description, timestamp or location similarity. No mutable organization balance or real-money movement.
- OpenAPI is the HTTP source of truth; JSON Schema is the event/payload contract source. Compatibility changes require old/new contract tests and version/rollback evidence.
- Every adapter has local fakes; CI requires no live third-party credentials.
- No production backfill, destructive migration, payment movement, legal/tax/retention policy invention or production provider activation.
- No feature is accepted merely because its PR merges. Record exact-head PostgreSQL, contract, security and required CI evidence; maintainers decide acceptance.

## 6. Coordinator verification checklist

- [ ] Re-read current main and every candidate PR diff before each integration.
- [ ] Confirm exact owner and file allowlist for each module-private worker.
- [ ] One active writer for each migration number and each shared OpenAPI/HTTP/store file.
- [ ] PostgreSQL migration up/down rehearsal on disposable database; migration runner repeatability.
- [ ] PostgreSQL concurrency, tenant-isolation, authorization and immutability tests.
- [ ] OpenAPI parse/lint and backward-compatibility check; JSON Schema validation.
- [ ] Event contract tests cover every actual consumer, not only in-repo fixtures.
- [ ] `make verify` and `make verify-full` outcomes recorded exactly; no inferred passes.
- [ ] Exact-head required CI/security checks green after final integration commit.
- [ ] Dependent issues unblocked only with exact artifact links and passing evidence.
- [ ] No `feature_list.json` acceptance mutation by module workers or this queue.

**Inventory result:** queue established; shared implementation is not yet integrated. The only safe immediate work is module-private development within existing PR ownership and the explicit Q0 reconciliation. No dependent issue is unblocked by this document alone.
