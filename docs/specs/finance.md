# M6 — Finance and Payment Specification

**Feature IDs:** FIN-001–FIN-004  
**Status:** ledger, payment-operation and reconciliation foundations exist. No production payment provider, capture, escrow movement, refund execution or settlement integration is enabled.

## Ownership

Finance owns ledger accounts, immutable journals and entries, payment operation state, webhook receipts, payment-to-ledger links and settlement reconciliation. Implementation:
- `ledger.go`, `payment.go`, `reconciliation.go`
- migrations `009_ledger_accounts.sql` through `012_settlement_reconciliation.sql`

## Invariants

1. Amounts are positive integer minor units and every amount has an explicit ISO currency.
2. A journal contains at least two entries; debit and credit totals balance per currency.
3. Journal idempotency keys are unique; same key + same canonical payload replays; changed payload conflicts.
4. Journal and entry records are append-only. Corrections use one compensating reversal; a journal cannot be reversed twice.
5. Payment operations have durable idempotency keys and explicit states. Unknown provider outcomes remain unknown until reconciled; timeout is not proof of failure.
6. Webhook signatures are verified before processing. Event IDs are deduplicated; same event ID with changed payload conflicts.
7. A successful payment operation can link to only one ledger journal, and currency/debit/credit totals must match the operation amount.
8. Reconciliation reports differences; it must not silently mutate the ledger or invent adjustments.
9. Test/fake adapters are not production providers and must not be presented as real payment capability.

## Provider boundary

All real providers implement the provider-neutral contract through an adapter. Provider-specific credentials, webhook secrets and remote references are secret material. Retries reuse the exact same idempotency key. Unknown outcomes require provider query/reconciliation before any new operation.

## Verification

- `ledger_test.go`: balance, replay, changed payload, reversal, disabled/unknown accounts and database immutability.
- `payment_test.go`: fake provider idempotency, timeouts, declines, webhook signature and persistence.
- `reconciliation_test.go`: matched/unmatched, amount/currency differences, statement replay and no ledger auto-adjustment.

## Commission, incentive and credit rules

FIN-008 adds versioned market-scoped commission, reward, penalty, coupon and customer-credit rules. Each applied adjustment must be traceable to its rule version and represented by balanced immutable ledger postings or an explicit non-cash entitlement. Historical postings are reversed through compensating entries, never edited. The feature remains planned until implementation and maintainer review of automated finance evidence.

## Release gates and open gaps

- No production provider adapter or funds movement is enabled in the currently implemented API.
- Add independent tests for concurrent same-key operation creation, duplicate/out-of-order webhooks, unknown-outcome recovery and concurrent journal reversal.
- Define accounting policy, charge/refund lifecycle and reconciliation ownership before any real-money workflow.
- Production enablement requires explicit provider/security/legal review and human approval; a market configuration entry is not authorization.


## Project acceptance policy

No separate independent QA operator, acceptance worker, or external sign-off is required. The project maintainer accepts work after reviewing implementation, automated tests/evaluations, exact-head required CI/security checks, and recorded evidence. Technical gates remain mandatory; merge alone is not acceptance.


## Invoice and market tax record boundary (FIN-007)

Invoice snapshots preserve the source order, immutable price-version reference, explicit currency, source line ordering and amounts, classified billable-ledger semantics, journal/payment references, and each tax component's exact market/jurisdiction/category/configuration revision, effective interval, rational rate, rounding method, approval provenance and calculated result. Monetary calculations use integer minor units and overflow-safe integer arithmetic only.

Tax resolution is exact on market, jurisdiction, category, component, currency and effective time. Missing or overlapping active rules fail closed. There is no inferred rate, tax category, jurisdiction, legal entity, currency conversion, fallback market, compounding order, invoice wording/numbering rule, filing rule or retention period. Taxable bases must be supplied explicitly by the reviewed policy boundary. Approval metadata in a tax-rule snapshot is evidence, not authorization; the persistence/application boundary must verify the authorized tax-config revision before the domain calculator receives it.

Issued snapshots are append-only. A correction is a new linked snapshot with a reason; a financial correction must reference compensating journal postings. Existing invoices, source price snapshots, tax rule revisions and ledger postings are never rewritten. Same-key/same-content requests replay; changed content conflicts.

The current module-private domain implementation in `internal/marketplace/invoices.go` supplies deterministic tax arithmetic and snapshot validation only. Durable invoice/tax tables, immutable database triggers, tenant-scoped read/issue/correction/export authorization, source Order→Payment→Journal attribution, HTTP/OpenAPI and PostgreSQL concurrency/immutability tests remain shared integration work coordinated by #232. Do not issue invoices from sources lacking explicit validated attribution. No real invoice, production tax submission or funds movement is enabled.
