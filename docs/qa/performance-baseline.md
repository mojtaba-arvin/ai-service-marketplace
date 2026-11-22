# Bounded performance baseline

Run a small, read-only request baseline against an API deployment:

```sh
BASE_URL=https://market.example.com REQUESTS=100 CONCURRENCY=10 \
  ./scripts/perf-health-baseline.sh
```

The script is deliberately bounded (1–5,000 requests; concurrency 1–50), performs only GET requests to `/healthz`, rejects non-200 responses, and reports observed p50/p95 latency. It has no built-in latency threshold because no representative production workload or SLO has been approved.

**Interpretation:** `/healthz` performs a database health check. This is an infrastructure/readiness baseline, not a marketplace throughput benchmark. It does not measure booking, dispatch, authentication, analytics queries, write contention, or browser performance. Do not use these results to claim production capacity. A representative load profile and resource/query budgets must be defined before F8 can be accepted.
