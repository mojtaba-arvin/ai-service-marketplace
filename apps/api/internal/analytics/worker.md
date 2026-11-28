# Analytics projection worker

Runs one tenant's active projection generation. Configure DATABASE_URL, ANALYTICS_TENANT_ID, ANALYTICS_GENERATION_ID and ANALYTICS_WORKER_ID. The worker polls bounded batches of at most 100 canonical tenant events not yet present in the generation-specific processed_events consumer and uses outbox.Consumer.Process so deduplication and projection writes share one transaction. It exits on SIGTERM.

On first startup it creates/activates the requested generation only if the tenant has no active generation. If another generation is active it fails closed. Legacy outbox payloads without a canonical tenant_id are not consumed.

This worker handles the active live tail and never includes out-of-policy late events in metrics. Historical replay and cutover are performed by the separate `/app/analytics-rebuild` command documented below. Do not reuse an active or retired generation ID.

## Production Compose

Set `ANALYTICS_TENANT_ID`, `ANALYTICS_GENERATION_ID` and `ANALYTICS_WORKER_ID` in the untracked production environment file, then enable the optional service with:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml --profile analytics up -d analytics-worker
docker compose --env-file .env.production -f infra/compose.production.yml logs --tail=100 analytics-worker
```

The profile remains disabled by default. The first run may establish the tenant's initial active generation; if one already exists, the worker only accepts that exact generation and consumer identity.

Analytics summary readiness includes the active generation's pending canonical-event count. `data_ready` is false while that generation still has unprocessed tenant events, so partial catch-up is not presented as a complete projection.


## Historical generation rebuild

Use the one-shot `/app/analytics-rebuild` command to build a new shadow generation from all canonical tenant-scoped outbox rows. It uses a generation-specific `processed_events` identity and repeatedly polls rows that are still unprocessed; it deliberately does not advance an outbox-ID watermark because PostgreSQL sequence allocation order is not commit order.

Required environment:
- `DATABASE_URL`
- `ANALYTICS_TENANT_ID`
- `ANALYTICS_GENERATION_ID` (a new, never-active generation ID)
- `ANALYTICS_REBUILD_CONFIRM`, which must exactly equal `ANALYTICS_TENANT_ID:ANALYTICS_GENERATION_ID`

A failed run can resume the same generation only while its status remains `building` and the consumer identity is unchanged. Do not reuse active or retired generations. The command validates that every tenant-scoped source row is processed and has exactly one fact or quarantine outcome, and refuses cutover while any committed source event visible to the cutover transaction is pending. Generation status and the tenant active pointer switch in one transaction.

By default, events older than the metric family's 72-hour allowed-lateness boundary are retained as late facts but excluded from metrics. To perform an explicit correction replay, set `ANALYTICS_INCLUDE_LATE_EVENTS=true` for the one-shot command and use a fresh generation ID. This is a metric correction, not retention or financial-close certification. Review the late-event counts and source checksum before cutover; the old generation remains retained for rollback. Never set this flag on the live worker.

After successful cutover, restart the live analytics worker with the same tenant and the new generation ID. It polls all unprocessed events without a monotonic cursor, so commits that become visible after the rebuild's final scan are picked up during live catch-up. Keep the previous generation as a rollback target; do not delete it as part of routine rebuild.

The rebuild is bounded per query (100 rows) but is a full-history operation and may run for a long time. Run one rebuild per tenant at a time and monitor database load. A nonzero quarantine count is visible and included in validation; it does not mean those events contribute to metrics. The command applies the shared event-time correction policy and preserves prior generations; it does not certify financial close, legal retention, or contractual completion.


### Running rebuild and rollback with production Compose

Build the new API image first so the one-shot binary is present:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml build api
docker compose --env-file .env.production -f infra/compose.production.yml --profile analytics run --rm --no-deps --entrypoint /app/analytics-rebuild analytics-worker
```

Before running, set `ANALYTICS_GENERATION_ID` to a fresh generation ID and `ANALYTICS_REBUILD_CONFIRM` to the exact value `tenant-id:generation-id` in the production environment file. The confirmation is a guard against running a rebuild with the wrong tenant/generation pair, not an authorization mechanism. After a successful cutover, keep that generation ID and recreate the worker:

```sh
docker compose --env-file .env.production -f infra/compose.production.yml --profile analytics up -d --no-deps --force-recreate analytics-worker
```

To roll back, set `ANALYTICS_GENERATION_ID` to the retained previous generation, set `ANALYTICS_REBUILD_MODE=rollback`, and update the confirmation to the same `tenant-id:generation-id` pair. Run the one-shot command again. It catches the retired generation up from unprocessed canonical events, validates its complete fact/quarantine coverage, and atomically restores its active pointer. Then recreate the worker using that restored generation ID. Remove or clear `ANALYTICS_REBUILD_MODE` after the rollback.

The command never deletes the previous generation. Keep it until the new generation has been operationally observed and an explicit retention policy exists.
