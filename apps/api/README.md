# Core API — Walking Skeleton

This is an isolated **development/test harness**, not a production identity or payment implementation.

## Run locally

From the repository root:

```sh
docker compose up -d postgres
cd apps/api
export APP_ENV=test
export DATABASE_URL='postgres://marketplace:marketplace@localhost:5432/marketplace?sslmode=disable'
go run ./cmd/api
```

The server applies the initial SQL schema on startup. It refuses to start with `APP_ENV=production` because the only identity adapter is deliberately fake.

## Test-only workflow

1. `POST /v1/test/otp/verify` with `{"phone":"+96890000000","code":"000000"}` returns a test bearer token. Override the code with `TEST_OTP_CODE`.
2. `POST /v1/orders` with `Authorization: Bearer test-session`, a unique `Idempotency-Key`, and `{"amount_minor":1200,"currency":"OMR"}` creates an order.
3. Repeating the same request returns the original order. Reusing the key with a different amount/currency is rejected.
4. `POST /v1/orders/{id}/transition` with `{"to":"accepted"}` advances the guarded state machine. Invalid transitions return conflict.
5. Order, simulated ledger journal/entries, and outbox event are committed in one PostgreSQL transaction.

The ledger posting is simulated test data. It does not authorize, capture, hold, transfer, or settle real funds. There is no live SMS, payment, KYC, or production auth integration.

## Integrity boundaries

- Money uses positive integer minor units and an explicit three-letter currency.
- A deferred PostgreSQL constraint trigger checks journal debit/credit balance at commit.
- Ledger journals and entries are append-only; corrections require compensating journals.
- Order idempotency is enforced by a database unique constraint scoped to customer.
- Outbox records are inserted in the same transaction as order state changes.
- `processed_events` provides a unique consumer/event key for a future idempotent consumer; publisher/consumer worker and end-to-end harness remain outstanding.
