# M6 — Finance and Payment Specification

**Feature IDs:** FIN-001–FIN-006  
**Status:** FIN-001–004 foundations exist; FIN-006 adds a provider-neutral, human-approved payout/refund/chargeback case record and reconciliation boundary. No production payment provider or real-money movement is enabled.

## Ownership

Finance owns ledger accounts, immutable journals and entries, payment operation state, webhook receipts, payment-to-ledger links and settlement reconciliation. Implementation:
- `ledger.go`, `payment.go`, `reconciliation.go`, `finance_cases.go`
- migrations `009_ledger_accounts.sql` through `012_settlement_reconciliation.sql`, plus `037_finance_case_workflows.sql`

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


## FIN-006 — Payout, refund and chargeback cases

- Order/payment attribution is established once through an explicitly authorized `BindPaymentOperationToOrder` operation. It verifies the order tenant, successful capture, exact order/payment amount and currency, and the linked balanced source journal; an append-only binding event and composite foreign keys preserve the relationship. Legacy payment operations remain unattributed until explicitly bound; no identity/amount/time heuristic backfills them.
- A case snapshots that tenant-bound order, successful source payment operation, source journal, provider code, provider-scoped idempotency key, integer minor-unit amount and currency. Cases require the composite order/payment attribution and the explicit payment-to-journal link.
- The lifecycle is `requested → accepted → provider_confirmed → reconciled`, with a separate `failed` terminal outcome. `accepted` requires a separately authorized operator and reason; the requester cannot approve their own case.
- An ambiguous timeout is recorded as `provider_outcome=unknown` while the case remains accepted. It is not a failure or confirmation and keeps the original provider idempotency key. A read-only query contract (implemented by the deterministic FIN-003 fake provider for tests) resolves unknown outcomes outside the DB transaction, then rechecks authorization/state under a row lock; query errors or unknown results leave the case unresolved.
- Provider callbacks pass through the existing FIN-003 `PaymentProvider.VerifyWebhook` contract via a provider-neutral finance-case adapter, are deduplicated by provider/event ID and payload hash, and are bound to the tenant from the matched case—not from callback input. Changed-payload replay conflicts; out-of-order state changes cannot regress a terminal case.
- A provider confirmation is not ledger reconciliation. Reconciliation links only an existing balanced journal with exact amount/currency. Missing journals, amount/currency mismatches and invalid payout reversals are recorded as append-only discrepancy checks; reconciliation never creates or edits a journal.
- Payout reversal is a distinct case that must reference a previously reconciled payout and reconcile to a compensating journal that reverses that payout journal. The original payout and ledger postings remain immutable.
- Chargeback case tracking does not decide liability, evidence sufficiency, appeal outcome or scheme deadlines. Those remain separate human-reviewed dispute decisions under TRUST-003 / #320.
- These Store workflows record and reconcile evidence only. They do not invoke a live provider, submit a payout/refund/chargeback, or move funds. Any future provider command requires a separately reviewed adapter and an explicit human approval boundary.

## Commission, incentive and credit rules

FIN-008 adds versioned market-scoped commission, reward, penalty, coupon and customer-credit rules. Each applied adjustment must be traceable to its rule version and represented by balanced immutable ledger postings or an explicit non-cash entitlement. Historical postings are reversed through compensating entries, never edited. The feature remains planned until implementation and maintainer review of automated finance evidence.

## Release gates and open gaps

- No production provider adapter or funds movement is enabled in the currently implemented API.
- FIN-006 case persistence and Store operations are not a production payment capability; authenticated API/webhook route integration, production adapter selection and adopter fraud-rule configuration remain gated.
- Add independent tests for concurrent same-key operation creation, duplicate/out-of-order webhooks, unknown-outcome recovery and concurrent journal reversal.
- Define accounting policy, charge/refund lifecycle and reconciliation ownership before any real-money workflow.
- Production enablement requires explicit provider/security/legal review and human approval; a market configuration entry is not authorization.


## Project acceptance policy

No separate independent QA operator, acceptance worker, or external sign-off is required. The project maintainer accepts work after reviewing implementation, automated tests/evaluations, exact-head required CI/security checks, and recorded evidence. Technical gates remain mandatory; merge alone is not acceptance.
