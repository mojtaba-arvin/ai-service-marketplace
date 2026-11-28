package analytics

import (
    "context"
    "errors"
    "strings"
    "time"

    "github.com/jackc/pgx/v5"
)

var ErrInvalidSummaryFilter = errors.New("invalid analytics summary filter")

type SummaryFilter struct {
    From time.Time
    To time.Time
    MarketCode string
}

type Summary struct {
    WindowFrom time.Time `json:"window_from"`
    WindowTo time.Time `json:"window_to"`
    MarketCode string `json:"market_code,omitempty"`
    DataReady bool `json:"data_ready"`
    EligibleEvents int64 `json:"eligible_events"`
    PendingEvents int64 `json:"pending_events"`
    EventTimeCompleteness string `json:"event_time_completeness"`
    EventTimeWatermark time.Time `json:"event_time_watermark"`
    AllowedLatenessSeconds int64 `json:"allowed_lateness_seconds"`
    LateEvents int64 `json:"late_events"`
    LateEventsIncluded int64 `json:"late_events_included"`
    QuarantinedEvents int64 `json:"quarantined_events"`
    IngestionBacklogStatus string `json:"ingestion_backlog_status"`
    SourceCoverageStatus string `json:"source_coverage_status"`
    ExcludedEvents int64 `json:"excluded_events"`
    OrdersCreated int64 `json:"orders_created"`
    OffersCreated int64 `json:"offers_created"`
    OffersAccepted int64 `json:"offers_accepted"`
    OrdersCancelled int64 `json:"orders_cancelled"`
    OrdersCompleted int64 `json:"orders_completed"`
    OfferAcceptanceRate float64 `json:"offer_acceptance_rate"`
    CancellationRate float64 `json:"cancellation_rate"`
    CompletionRate float64 `json:"completion_rate"`
    MedianAssignmentSeconds float64 `json:"median_assignment_seconds"`
    P90AssignmentSeconds float64 `json:"p90_assignment_seconds"`
}

type queryRower interface { QueryRow(context.Context, string, ...any) pgx.Row }

func QuerySummary(ctx context.Context, db queryRower, tenantID string, filter SummaryFilter) (Summary, error) {
    tenantID=strings.TrimSpace(tenantID)
    filter.MarketCode=strings.TrimSpace(filter.MarketCode)
    if db==nil || tenantID=="" || filter.From.IsZero() || filter.To.IsZero() ||
        !filter.To.After(filter.From) || filter.To.Sub(filter.From)>90*24*time.Hour ||
        (filter.MarketCode!="" && !marketCodePattern.MatchString(filter.MarketCode)) {
        return Summary{},ErrInvalidSummaryFilter
    }
    from,to:=filter.From.UTC(),filter.To.UTC()
    var result Summary
    result.WindowFrom,result.WindowTo,result.MarketCode=from,to,filter.MarketCode
    err:=db.QueryRow(ctx,`
WITH active_generation AS (
 SELECT ag.tenant_id,ag.generation_id,g.consumer_name FROM analytics_active_generations ag JOIN analytics_projection_generations g ON g.tenant_id=ag.tenant_id AND g.generation_id=ag.generation_id WHERE ag.tenant_id=$1
),
active_analytics_event_facts AS (
 SELECT f.* FROM analytics_event_facts f JOIN active_generation ag ON ag.tenant_id=f.tenant_id AND ag.generation_id=f.generation_id
),
created_orders AS (
    SELECT DISTINCT ON (tenant_id,data->>'order_id',market_code)
           tenant_id,data->>'order_id' AS order_id,market_code,occurred_at AS created_at
    FROM active_analytics_event_facts
    WHERE tenant_id=$1 AND event_type='order.created' AND traffic_class='production'
      AND market_code IS NOT NULL AND included_in_metrics=true AND occurred_at >= $2 AND occurred_at < $3
      AND ($4='' OR market_code=$4) AND NULLIF(data->>'order_id','') IS NOT NULL
    ORDER BY tenant_id,data->>'order_id',market_code,occurred_at,event_id
),
created_offers AS (
    SELECT DISTINCT ON (tenant_id,data->>'offer_id',market_code)
           tenant_id,data->>'offer_id' AS offer_id,data->>'order_id' AS order_id,
           market_code,occurred_at AS created_at,(data->>'expires_at')::timestamptz AS expires_at
    FROM active_analytics_event_facts
    WHERE tenant_id=$1 AND event_type='dispatch.offer_created' AND traffic_class='production'
      AND market_code IS NOT NULL AND included_in_metrics=true AND occurred_at >= $2 AND occurred_at < $3
      AND ($4='' OR market_code=$4) AND NULLIF(data->>'offer_id','') IS NOT NULL
      AND NULLIF(data->>'expires_at','') IS NOT NULL
    ORDER BY tenant_id,data->>'offer_id',market_code,occurred_at,event_id
),
assignments AS (
    SELECT DISTINCT tenant_id,data->>'order_id' AS order_id,data->>'offer_id' AS offer_id,
           market_code,occurred_at
    FROM active_analytics_event_facts
    WHERE tenant_id=$1 AND event_type='order.provider_assigned' AND traffic_class='production'
      AND market_code IS NOT NULL AND included_in_metrics=true AND ($4='' OR market_code=$4)
),
accepted_offers AS (
    SELECT DISTINCT o.offer_id
    FROM created_offers o JOIN assignments a
      ON a.tenant_id=o.tenant_id AND a.offer_id=o.offer_id AND a.order_id=o.order_id
     AND a.market_code=o.market_code AND a.occurred_at >= o.created_at AND a.occurred_at <= o.expires_at
),
assignment_durations AS (
    SELECT EXTRACT(EPOCH FROM (MIN(a.occurred_at)-o.created_at)) AS seconds
    FROM created_orders o JOIN assignments a
      ON a.tenant_id=o.tenant_id AND a.order_id=o.order_id AND a.market_code=o.market_code
     AND a.occurred_at >= o.created_at
    GROUP BY o.tenant_id,o.order_id,o.created_at
)
SELECT
    (SELECT count(*) FROM created_orders),
    (SELECT count(*) FROM created_offers),
    (SELECT count(*) FROM accepted_offers),
    (SELECT count(*) FROM created_orders o WHERE EXISTS (
        SELECT 1 FROM active_analytics_event_facts c
        WHERE c.tenant_id=o.tenant_id AND c.event_type='order.cancelled'
          AND c.traffic_class='production' AND c.included_in_metrics=true AND c.market_code=o.market_code
          AND c.data->>'order_id'=o.order_id AND c.occurred_at>=o.created_at
    )),
    (SELECT count(*) FROM created_orders o WHERE EXISTS (
        SELECT 1 FROM active_analytics_event_facts c
        WHERE c.tenant_id=o.tenant_id AND c.event_type='order.state_changed'
          AND c.traffic_class='production' AND c.included_in_metrics=true AND c.market_code=o.market_code
          AND c.data->>'order_id'=o.order_id AND c.data->>'to'='completed'
          AND c.occurred_at>=o.created_at
    )),
    COALESCE((SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY seconds) FROM assignment_durations),0),
    COALESCE((SELECT percentile_cont(0.9) WITHIN GROUP (ORDER BY seconds) FROM assignment_durations),0),
    (SELECT count(*) FROM active_analytics_event_facts WHERE tenant_id=$1 AND occurred_at >= $2 AND occurred_at < $3
       AND traffic_class='production' AND market_code IS NOT NULL AND included_in_metrics=true AND ($4='' OR market_code=$4)),
    (SELECT count(*) FROM active_analytics_event_facts WHERE tenant_id=$1 AND occurred_at >= $2 AND occurred_at < $3
       AND (traffic_class IS DISTINCT FROM 'production' OR market_code IS NULL)
       AND ($4='' OR market_code=$4 OR market_code IS NULL)),
    (SELECT count(*) FROM outbox_events e JOIN active_generation ag ON ag.tenant_id=$1
       WHERE e.payload->>'tenant_id'=$1
         AND NOT EXISTS (SELECT 1 FROM processed_events p WHERE p.consumer_name=ag.consumer_name AND p.event_id=e.id)),
    (SELECT count(*) FROM active_analytics_event_facts WHERE tenant_id=$1 AND occurred_at >= $2 AND occurred_at < $3
       AND event_time_family='order_funnel' AND late_arrival=true AND ($4='' OR market_code=$4 OR market_code IS NULL)),
    (SELECT count(*) FROM active_analytics_event_facts WHERE tenant_id=$1 AND occurred_at >= $2 AND occurred_at < $3
       AND event_time_family='order_funnel' AND late_arrival=true AND included_in_metrics=true AND ($4='' OR market_code=$4 OR market_code IS NULL)),
    (SELECT count(*) FROM analytics_event_quarantine q JOIN active_generation ag ON ag.tenant_id=q.tenant_id AND ag.generation_id=q.generation_id
       WHERE q.tenant_id=$1 AND (q.occurred_at IS NULL OR (q.occurred_at >= $2 AND q.occurred_at < $3))
         AND ($4='' OR q.market_code=$4 OR q.market_code IS NULL)),
    clock_timestamp() - interval '72 hours'
`,tenantID,from,to,filter.MarketCode).Scan(
        &result.OrdersCreated,&result.OffersCreated,&result.OffersAccepted,
        &result.OrdersCancelled,&result.OrdersCompleted,
        &result.MedianAssignmentSeconds,&result.P90AssignmentSeconds,
        &result.EligibleEvents,&result.ExcludedEvents,&result.PendingEvents,&result.LateEvents,&result.LateEventsIncluded,&result.QuarantinedEvents,&result.EventTimeWatermark)
    if err!=nil { return Summary{},err }
    result.DataReady=result.EligibleEvents>0 && result.PendingEvents==0
    policy,_:=EventTimePolicyFor("order.created")
    result.AllowedLatenessSeconds=int64(policy.AllowedLateness.Seconds())
    result.EventTimeWatermark=result.EventTimeWatermark.UTC()
    switch {
    case result.QuarantinedEvents>0:
        result.EventTimeCompleteness="incomplete"
    case result.LateEvents>result.LateEventsIncluded:
        result.EventTimeCompleteness="correction_required"
    case result.ExcludedEvents>0:
        result.EventTimeCompleteness="incomplete"
    case result.EligibleEvents==0:
        result.EventTimeCompleteness="unavailable"
    case result.WindowTo.After(result.EventTimeWatermark):
        result.EventTimeCompleteness="immature"
    case result.LateEvents>0:
        result.EventTimeCompleteness="corrected"
    default:
        result.EventTimeCompleteness="finalized"
    }
    if result.PendingEvents==0 { result.IngestionBacklogStatus="drained" } else { result.IngestionBacklogStatus="pending" }
    result.SourceCoverageStatus="unknown"
    if result.QuarantinedEvents>0 || result.ExcludedEvents>0 { result.SourceCoverageStatus="incomplete" } else if result.EligibleEvents==0 { result.SourceCoverageStatus="unavailable" }
    if result.OffersCreated>0 { result.OfferAcceptanceRate=float64(result.OffersAccepted)/float64(result.OffersCreated) }
    if result.OrdersCreated>0 {
        result.CancellationRate=float64(result.OrdersCancelled)/float64(result.OrdersCreated)
        result.CompletionRate=float64(result.OrdersCompleted)/float64(result.OrdersCreated)
    }
    return result,nil
}
