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
