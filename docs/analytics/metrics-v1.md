# Analytics metric contracts v1

**Status: metric contract with partial runtime implementation.** PRs #194/#196/#198 provide the tenant/generation-scoped projection, summary API and guarded rebuild/rollback. This document does not claim full producer adoption, all metric families, event-time finality, dashboards or maintainer acceptance.

Machine-readable source: [metrics-v1.json](metrics-v1.json)

## Global rules

- Partition every projection by `tenant_id`; never aggregate across tenants.
- Use canonical event `id` for delivery deduplication and `occurred_at` for event time.
- Only events with `metadata.traffic_class = production` enter production metrics. Missing, demo, and test classifications are excluded rather than guessed.
- Resolve daily buckets using the IANA timezone of the event's market configuration, not the worker or database session timezone.
- The 72-hour watermark is a proposed v1 policy, not implemented runtime behavior. Until #314 is complete, do not claim event-time finality or silently discard late events; affected historical metrics remain potentially revisable.
- Replay in stable `occurred_at, event.id` order. Duplicate deliveries must not change totals.
- Unknown schema versions and events with prohibited personal-data fields are quarantined or rejected.
- Keep currency-separated dimensions; never sum monetary amounts across currencies.

## Metric definitions

| Metric | Numerator | Denominator | Cohort |
|---|---|---|---|
| Orders created | Distinct canonical `order.created` event IDs | None | Order creation time |
| Offer acceptance rate | Distinct accepted `offer_id` values from `order.provider_assigned` | Distinct `offer_id` values from `dispatch.offer_created` | Offer creation time |
| Order cancellation rate | Distinct cancelled order IDs | Distinct created order IDs | Order creation time |
| Order completion rate | Distinct order IDs reaching `completed` | Distinct created order IDs | Order creation time |
| Time to assignment | Median and p90 of assignment time minus creation time | Orders with one valid creation and assignment | Order creation time |
| Verified provider supply | Providers verified and not deactivated at snapshot time | None | Daily event-time snapshot |

## Important limitations

1. A created-order count is not a request-to-order conversion rate. The latter requires a separately defined, canonical service-request event.
2. Offer acceptance must match the created offer by `offer_id`; an assignment without a matching offer is not counted.
3. Cancellation and completion are cohort metrics, not counts of all transitions occurring during the day.
4. Provider supply cannot be calculated from the current event set. Canonical provider verification/deactivation events are required; analytics must not silently query operational provider tables.
5. Every metric remains marked blocked until its producer emits the required canonical envelope and traffic/market metadata. The JSON status fields are authoritative.

## Implementation sequence

1. Complete canonical producer adoption and schema compatibility (#87).
2. Add the event consumer with transactional deduplication, quarantine, and deterministic replay.
3. Implement tenant-partitioned read models from these contracts.
4. Reconcile every projection against source events and adversarial replay fixtures before exposing an API.
5. Implement and verify the event-time completeness/late-arrival contract in [#314](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/314).
