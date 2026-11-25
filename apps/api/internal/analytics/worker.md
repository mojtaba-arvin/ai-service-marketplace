# Analytics projection worker

Runs one tenant's active projection generation. Configure DATABASE_URL, ANALYTICS_TENANT_ID, ANALYTICS_GENERATION_ID and ANALYTICS_WORKER_ID. The worker polls bounded batches of at most 100 canonical tenant events not yet present in the generation-specific processed_events consumer and uses outbox.Consumer.Process so deduplication and projection writes share one transaction. It exits on SIGTERM.

On first startup it creates/activates the requested generation only if the tenant has no active generation. If another generation is active it fails closed. Legacy outbox payloads without a canonical tenant_id are not consumed.

This is a live-tail consumer, not a historical backfill or rebuild tool. Do not reuse a generation ID or switch the active pointer to rebuild. Safe rebuild still requires a quiescent historical snapshot, live-tail catch-up without sequence-watermark gaps, validation and an atomic pointer switch. That rebuild workflow is intentionally not claimed by this change.
