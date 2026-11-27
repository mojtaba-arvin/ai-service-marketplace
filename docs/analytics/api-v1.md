# Operator analytics summary API v1

**Endpoint:** `GET /v1/operator/analytics/summary`

**Access:** authenticated Operator in the active tenant. The tenant is always derived from the authenticated actor; callers cannot supply or override `tenant_id`.

## Query parameters

- `from`: required inclusive RFC3339 event-time lower bound.
- `to`: required exclusive RFC3339 event-time upper bound. The range is limited to 90 days.
- `market_code`: optional uppercase ISO-3166 alpha-2 market code.

## Response

The response contains order volume, offer volume and acceptance, cancellation/completion cohort rates, median/p90 time to assignment, and event-readiness fields.

- `data_ready` is true only when production-classified, market-attributed events exist in the requested window and the active generation has no pending canonical tenant events.
- `eligible_events` counts facts with `traffic_class=production` and a market code.
- `pending_events` counts canonical tenant-scoped outbox events not yet processed by the active generation; readiness remains false during catch-up.
- `excluded_events` counts facts that are not production-classified or lack market attribution.
- Rates are ratios in the range 0..1, not percentages.
- All counts and joins are tenant-scoped. Order/offer identity is deduplicated by business ID.
- Cohorts use event time, and assignments/cancellations/completions may arrive after the cohort window.
- The API returns `Cache-Control: private, no-store`.

## Readiness limitation

The v1 metric contract requires explicit `metadata.market_code` and `metadata.traffic_class=production`. Current canonical producers do not yet populate all required metadata. Such events are retained with null metadata and excluded; the API must not infer production traffic from a tenant ID, hostname, or request origin. A zero metric with `data_ready=false` means the source metadata is not yet ready, not proof that the tenant had no activity.

## Source of truth

The API reads only the append-only `analytics_event_facts` projection. It does not query orders, provider profiles, payments or ledger tables. Unsupported and legacy event shapes are quarantined and do not contribute to metrics.

### Readiness interpretation

`data_ready` describes ingestion lag at query time only. Historical shadow-generation rebuild/cutover/rollback is implemented by PR #198, but production operation and worker handoff are not evidenced. The proposed 72-hour event-time watermark and period invalidation/replay policy are not implemented yet (#314); late arrivals may change historical cohorts. Do not use this flag as event-time finality or a financial/contractual close signal.
