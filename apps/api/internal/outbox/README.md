# Transactional outbox delivery

## Write-side contract

Call `outbox.Append(ctx, tx, ...)` using the same PostgreSQL transaction that mutates the aggregate. If that transaction rolls back, the event rolls back too. Event IDs are stable publication idempotency keys.

## Publisher

Workers claim bounded batches with `FOR UPDATE SKIP LOCKED` and a finite lease. Publication occurs outside the claim transaction. Acknowledgement and failure updates require the lease owner. Failed events use capped exponential backoff and become dead-lettered after the configured attempt limit or immediately when classified permanent.

Delivery is at-least-once: a process can crash after a provider accepts an event but before acknowledgement. Every downstream adapter must deduplicate the stable event ID.

## Consumer

`Consumer.Process` inserts (consumer, event ID) into `processed_events` and invokes the handler in the same transaction. The handler must use the supplied transaction for its database side effects. A duplicate event is a no-op; a handler error rolls back both the marker and effects.

## Verification

Pure retry-policy tests are offline. PostgreSQL integration tests exercise leases, concurrent claims, acknowledgement, retry/dead-letter transitions, and idempotent consumption.
