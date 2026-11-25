# Analytics projection worker

Runs one tenant's active projection generation. Configure DATABASE_URL, ANALYTICS_TENANT_ID, ANALYTICS_GENERATION_ID and ANALYTICS_WORKER_ID. The worker polls bounded batches of at most 100 canonical tenant events not yet present in the generation-specific processed_events consumer and uses outbox.Consumer.Process so deduplication and projection writes share one transaction. It exits on SIGTERM.

On first startup it creates/activates the requested generation only if the tenant has no active generation. If another generation is active it fails closed. Legacy outbox payloads without a canonical tenant_id are not consumed.

This is a live-tail consumer, not a historical backfill or rebuild tool. Do not reuse a generation ID or switch the active pointer to rebuild. Safe rebuild still requires a quiescent historical snapshot, live-tail catch-up without sequence-watermark gaps, validation and an atomic pointer switch. That rebuild workflow is intentionally not claimed by this change.

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

A failed run can resume the same generation only while its status remains `building` and the consumer identity is unchanged. Do not reuse active or retired generations. The command validates that every tenant-scoped source row is processed and has exactly one fact or quarantine outcome, and refuses cutover while any committed source event is pending. Generation status and the tenant active pointer switch in one transaction.

After successful cutover, restart the live analytics worker with the same tenant and the new generation ID. It polls all unprocessed events without a monotonic cursor, so commits that become visible after the rebuild's final scan are picked up during live catch-up. Keep the previous generation as a rollback target; do not delete it as part of routine rebuild.

The rebuild is bounded per query (100 rows) but is a full-history operation and may run for a long time. Run one rebuild per tenant at a time and monitor database load. A nonzero quarantine count is visible and included in validation; it does not mean those events contribute to metrics. The command does not claim event-time finality or financial close.
