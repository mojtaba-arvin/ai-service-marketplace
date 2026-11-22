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

## Release gates and open gaps

- No production provider adapter or funds movement is enabled in the current MVP.
- Add independent tests for concurrent same-key operation creation, duplicate/out-of-order webhooks, unknown-outcome recovery and concurrent journal reversal.
- Define accounting policy, charge/refund lifecycle and reconciliation ownership before any real-money workflow.
- Production enablement requires explicit provider/security/legal review and human approval; a market configuration entry is not authorization.
