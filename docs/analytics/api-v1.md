# Operator analytics summary API v1

**Endpoint:** `GET /v1/operator/analytics/summary`

**Access:** authenticated Operator in the active tenant. The tenant is always derived from the authenticated actor; callers cannot supply or override `tenant_id`.

## Query parameters

- `from`: required inclusive RFC3339 event-time lower bound.
- `to`: required exclusive RFC3339 event-time upper bound. The range is limited to 90 days.
- `market_code`: optional uppercase ISO-3166 alpha-2 market code.

## Response

The response contains order volume, offer volume and acceptance, cancellation/completion cohort rates, median/p90 time to assignment, and event-readiness fields.

- `data_ready` remains an ingestion-readiness indicator: it is true only when production-classified, market-attributed facts exist in the requested window and the active generation has no pending canonical tenant events. It is not event-time completeness or finality.
- `eligible_events` counts facts with `traffic_class=production` and a market code.
- `pending_events` counts canonical tenant-scoped outbox events not yet processed by the active generation; readiness remains false during catch-up.
- `excluded_events` counts facts that are not production-classified or lack market attribution.
- Rates are ratios in the range 0..1, not percentages.
- All counts and joins are tenant-scoped. Order/offer identity is deduplicated by business ID.
- Cohorts use event time, and assignments/cancellations/completions may arrive after the cohort window.
- The API returns `Cache-Control: private, no-store`.

## Event time, ingestion time and finality

- `occurred_at` is the canonical producer-supplied event time and is retained unchanged in every generation. `ingested_at` is the source outbox row's persisted `created_at`; `projected_at` remains the consumer processing timestamp. These timestamps are not interchangeable.
- The current versioned 72-hour starting lateness policy applies to `order_funnel` and `provider_supply` only. `service_quality` and `finance` fail closed until #299 defines its cohort maturity/late-event contract and #301 defines accounting-period/effective-time/late-posting rules. This is not a universal/legal retention rule, financial close, or contractual SLA. Metric owners may revise a family policy only through a versioned contract change.
- The watermark is database wall-clock UTC minus the family's allowed lateness. A requested period `[from,to)` is mature/finalizable only when `to <= watermark`. The watermark never uses outbox sequence IDs or maximum observed event time.
- Events at the exact 72-hour boundary are accepted; events older than the boundary are recorded as late and excluded from the active metric generation by default. Delayed events inside the window remain eligible and are cohort-assigned by `occurred_at`.
- A source `occurred_at` later than its persisted outbox `created_at` is quarantined as clock skew. No unapproved skew tolerance is assumed. The original source timestamp and payload digest remain in the source outbox for diagnosis/replay.
- Duplicate event IDs with identical payload digests are no-ops. A conflicting event-ID/payload pair is quarantined. Reordered delivery is deterministic because cohorts use event time and stable event IDs; generation replay reuses the original outbox ingestion timestamp.
- A finalized period is not silently rewritten. Late facts remain visible as `late_events` and set `event_time_completeness=correction_required`. A correction is an explicit new shadow generation with `ANALYTICS_INCLUDE_LATE_EVENTS=true`, followed by the existing guarded validation and atomic cutover. The prior generation remains available; the generation ID and source digest provide correction provenance.
- The response separates `ingestion_backlog_status`, `source_coverage_status` and `event_time_completeness`. Source coverage remains `unknown` unless producer coverage is proven; missing market/traffic metadata remains incomplete/unavailable and never becomes zero activity. The current order summary uses the `order_funnel` policy; #297, #299 and #301 must bind their own metric definitions to the same completeness vocabulary and their respective event-family policies.
- Period boundaries and market-local calendar interpretation belong to each metric contract. This API's RFC3339 range is normalized to UTC; supply daily snapshots must use persisted market timezone, not an inferred timezone.

## Readiness limitation

The v1 metric contract requires explicit `metadata.market_code` and `metadata.traffic_class=production`. Current canonical producers do not yet populate all required metadata. Such events are retained with null metadata and excluded; the API must not infer production traffic from a tenant ID, hostname, or request origin. A zero metric with `data_ready=false` means the source metadata is not yet ready, not proof that the tenant had no activity.

## Source of truth

The API reads only the append-only `analytics_event_facts` projection. It does not query orders, provider profiles, payments or ledger tables. Unsupported and legacy event shapes are quarantined and do not contribute to metrics.

### Readiness interpretation

`data_ready` describes ingestion lag at query time only. It does not certify event-time finality: late-arriving events may change historical cohorts, and a deterministic historical rebuild/cutover workflow is not yet implemented. Do not use this flag as a financial or contractual close signal.
