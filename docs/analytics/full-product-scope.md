# M12 analytics: implementation reconciliation and remaining scope

**Parent:** [Issue #16](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/16)  
**Canonical event work:** [Issue #87](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/87)  
**Recovered projection work:** [Issue #261](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/261)

This document distinguishes merged repository implementation from remaining product capabilities and operational/acceptance evidence. It does not mark any feature accepted.

## Existing implementation — preserve and extend

| Capability | Evidence on `main` | Verified boundary / limitation |
|---|---|---|
| Canonical event envelope and writer | ADR 0005; PR #95/#97 | Versioned tenant-scoped v1 envelope and transactional writer exist; legacy producers remain staged |
| Live analytics facts and quarantine | Migration 030; PR #194 | Tenant/generation-scoped facts, quarantine, immutable processed identity and bounded live polling |
| Operator summary API | PR #196 | Tenant derived from authenticated operator; `analytics:read`; bounded RFC3339 window; no-store |
| Historical rebuild and rollback | PR #198; analytics-rebuild command | Shadow generation, no sequence-ID watermark, coverage/hash validation, guarded atomic pointer cutover and retained rollback generation |
| Projection operations | PR #200; `apps/api/internal/analytics/worker.md` | Runbook exists; no production rebuild, cutover, worker handoff or load rehearsal is claimed |

The recovered Issue #261 branch and old migration 025 are superseded. Do not reintroduce its projector, summary API, migration or duplicate tests. The reserved Issue #16 branch was reconciled with current `main` using a non-force two-parent commit; its old commits remain in history.

## Correctness contract shared by every metric

1. **Provenance:** every fact is traceable to tenant-scoped canonical event ID, event type/version, occurrence time, aggregate identity, source payload digest, projection generation and an approved minimized data allowlist. Quarantine stores reason and digest, not raw rejected payload.
2. **Tenant isolation:** partition and join by `tenant_id`; derive it from the authorized aggregate context. Never accept tenant attribution from client analytics parameters or infer it from organization membership, host, location or identity.
3. **Deduplication:** delivery identity is `tenant_id + event.id` within a projection generation. Repeated identical deliveries are no-ops; an event ID reused with a different payload is a conflict/quarantine, not an update. Business ratios additionally deduplicate their business key (order, offer, provider, journal, review or experiment unit).
4. **Ordering and replay:** do not use PostgreSQL sequence allocation as commit order. Continue querying unprocessed source rows by generation-specific processed identity; replay deterministically by `occurred_at, event.id` where event-time ordering is required. Rebuild validates one source row -> one processed outcome -> exactly one fact or quarantine.
5. **Late events:** the metrics v1 document proposes a 72-hour event-time watermark, but current projector/summary code does not implement a finality watermark or period invalidation. Do not claim it does. A metric family must state how late events revise an original cohort, trigger replay, or remain incomplete; tests must include late commits and out-of-order delivery. `data_ready` is ingestion backlog status only.
6. **Missingness:** missing producer adoption, missing market/traffic metadata, absent events, quarantine or immature cohorts mean unavailable/incomplete. They are never silently converted into zero activity or a zero denominator.
7. **Numerators and denominators:** each rate documents eligible population, distinct business key, numerator, denominator, cohort anchor, event-time window, timezone, exclusions, maturity/finality and sample size. Volume metrics explicitly have no denominator. Financial values are integer minor units and currency-separated.

## Existing funnel metrics and gaps

The v1 contract documents orders created, offer acceptance, cancellation/completion cohorts and time to assignment. The summary API exposes these measures, but they remain incomplete where canonical producers or `market_code` / `traffic_class` metadata are missing. Provider supply is blocked until provider lifecycle events exist; do not query operational provider tables to fill the gap.

Canonical producer/compatibility work remains in:
- [#284 / PR #284](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/284): additive legacy payload compatibility; draft and exact-head verification pending.
- [#266 / PR #266](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/266): scheduled-order canonical producer; external sink compatibility remains a gate.
- [#276 / PR #276](https://github.com/mojtaba-arvin/ai-service-marketplace/pull/276): cancellation producers; merge blocked on additive compatibility and external sink evidence.

Do not merge overlapping producer changes twice. External compatibility requires a named sink/consumer owner and contract evidence or a versioned migration window; local permissive HTTP fixtures are not proof.

## Remaining independently owned capabilities

| Capability | Issue | Primary ownership | Dependencies |
|---|---|---|---|
| Event-time completeness and late-arrival policy | [#314](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/314) | Analytics completeness/replay | #87, PR #194/#198, AN-002 |
| Provider lifecycle and supply metrics | [#297](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/297) | Analytics projection; Provider/Trust event producer | #87, TRUST-001, DISP-001 and DISP-003 for availability/offer metrics |
| Service quality definitions and projection | [#299](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/299) | Analytics; Order/Review and Trust source events | #87, ORDER-002, TRUST-003 and TRUST-007 for adjudicated quality outcomes |
| Ledger-backed finance metrics | [#301](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/301) | Analytics read model; Finance event producer | FIN-001/002, #10, #57, ADR 0006 and #87; B2B dimensions additionally require #88 |
| Privacy-aware analytics export | [#302](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/302) | Analytics export; Platform/Identity privacy policy | AN-003, tenant authorization, consent/export/deletion policy |
| Controlled campaign/A-B experiments | [#303](https://github.com/mojtaba-arvin/ai-service-marketplace/issues/303) | Growth assignment/lifecycle; Analytics analysis | GROW-004, AN-001, GROW-001 where referral attribution is used |

The issue bodies contain the detailed metric definitions, explicit owner split, dependencies, non-goals and acceptance tests. The existing GROW-004 feature owns experimentation; it is not duplicated as a new AN feature.

## Architecture constraints

- Keep PostgreSQL as the current source/projection store. Do not add a warehouse, lake, broker, cache or paid analytics service without measured query/volume evidence, a concrete requirement and an ADR.
- Export approved projected facts only; never export raw outbox payloads by default.
- Quality analytics are descriptive and contestable, not an autonomous reputation/suspension authority.
- Finance analytics are read-only and reconcile to eligible immutable journal/entry IDs. They never post money, infer accounting treatment or certify financial close.
- B2B organization/property attribution is valid only through explicit same-tenant order and ledger references defined by ADR 0006. Membership is not tenant authorization.
- No dashboard should present a metric as zero when source coverage is missing or incomplete.

## Maintainer acceptance boundary

Implementation, integration, operational rehearsal and maintainer acceptance are distinct. No AN feature is marked passing here. Maintainer acceptance requires review of implementation, relevant positive/negative/concurrency and PostgreSQL tests, privacy/security evidence, exact-head required CI/security checks and any explicitly required operational rehearsal. The maintainer updates `feature_list.json` only after that evidence review.
