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

- `ledger_test.go`: balanced/overflow/cross-currency validation, same-key replay and concurrent changed-payload conflict, reversal replay and competing/shared-key reversal races, disabled/unknown accounts, UPDATE/DELETE immutability and TRUNCATE rejection.
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
