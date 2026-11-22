# Transactional outbox delivery

## Write-side contract

Call `outbox.AppendDomainEvent(ctx, tx, event)` for new tenant-scoped domain events using the same PostgreSQL transaction that mutates the aggregate. It validates the canonical v1 envelope, requires tenant and aggregate identity, and rejects prohibited sensitive field names recursively. If the transaction rolls back, the event rolls back too. The envelope's string `id` is the domain-event identity; the outbox row ID remains the transport/publication identity.

`outbox.Append` remains available only for staged legacy compatibility. Existing producers are not considered migrated until they derive tenant context from the authorized aggregate and emit the canonical envelope. The field-name guard is defense in depth, not a substitute for producer-specific data minimization or privacy review.

## Publisher

Workers claim bounded batches with `FOR UPDATE SKIP LOCKED` and a finite lease. Publication occurs outside the claim transaction. Acknowledgement and failure updates require the lease owner. Failed events use capped exponential backoff and become dead-lettered after the configured attempt limit or immediately when classified permanent.

Delivery is at-least-once: a process can crash after a provider accepts an event but before acknowledgement. Every downstream adapter must deduplicate the stable event ID.

## Consumer

`Consumer.Process` inserts (consumer, event ID) into `processed_events` and invokes the handler in the same transaction. The handler must use the supplied transaction for its database side effects. A duplicate event is a no-op; a handler error rolls back both the marker and effects.

## Verification

Pure retry-policy tests are offline. PostgreSQL integration tests exercise leases, concurrent claims, acknowledgement, retry/dead-letter transitions, and idempotent consumption.
