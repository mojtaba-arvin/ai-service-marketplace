# Runnable outbox publisher

`cmd/outbox-worker` claims leased events from PostgreSQL and delivers them to a configured HTTPS event sink. Each request carries the immutable outbox event ID as its `Idempotency-Key`; the receiver must persist that key and make repeated delivery a no-op.

The worker requires an explicit retry classifier: network errors, 408, 425, 429, and 5xx are retried with capped exponential backoff; other 4xx responses are dead-lettered immediately. Redirects are not followed, and credentials are never sent to a redirect target.

## Configuration

- `DATABASE_URL`
- `OUTBOX_PUBLISH_URL` (HTTPS only)
- `OUTBOX_PUBLISH_TOKEN`
- `OUTBOX_WORKER_ID` (stable unique ID per worker instance)

The worker does not run schema migrations; deploy after migration 019 has been applied by the API/migration step. It is safe to run multiple instances because claims use PostgreSQL row locks and leases.

This is a transport adapter boundary. A NATS JetStream publisher can implement the same interface without changing the transactional outbox or consumer contracts.
