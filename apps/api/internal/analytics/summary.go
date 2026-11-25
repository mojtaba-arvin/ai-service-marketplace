package analytics

import (
    "context"
    "errors"
    "strings"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
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

func QuerySummary(ctx context.Context,db *pgxpool.Pool,tenantID string,filter SummaryFilter)(Summary,error) {
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
WITH active_analytics_event_facts AS (
 SELECT f.* FROM analytics_event_facts f JOIN analytics_active_generations ag ON ag.tenant_id=f.tenant_id AND ag.generation_id=f.generation_id WHERE f.tenant_id=$1
),
created_orders AS (
    SELECT DISTINCT ON (tenant_id,data->>'order_id',market_code)
           tenant_id,data->>'order_id' AS order_id,market_code,occurred_at AS created_at
    FROM active_analytics_event_facts
    WHERE tenant_id=$1 AND event_type='order.created' AND traffic_class='production'
      AND market_code IS NOT NULL AND occurred_at >= $2 AND occurred_at < $3
      AND ($4='' OR market_code=$4) AND NULLIF(data->>'order_id','') IS NOT NULL
    ORDER BY tenant_id,data->>'order_id',market_code,occurred_at,event_id
),
created_offers AS (
    SELECT DISTINCT ON (tenant_id,data->>'offer_id',market_code)
           tenant_id,data->>'offer_id' AS offer_id,data->>'order_id' AS order_id,
           market_code,occurred_at AS created_at,(data->>'expires_at')::timestamptz AS expires_at
    FROM active_analytics_event_facts
    WHERE tenant_id=$1 AND event_type='dispatch.offer_created' AND traffic_class='production'
      AND market_code IS NOT NULL AND occurred_at >= $2 AND occurred_at < $3
      AND ($4='' OR market_code=$4) AND NULLIF(data->>'offer_id','') IS NOT NULL
      AND NULLIF(data->>'expires_at','') IS NOT NULL
    ORDER BY tenant_id,data->>'offer_id',market_code,occurred_at,event_id
),
assignments AS (
    SELECT DISTINCT tenant_id,data->>'order_id' AS order_id,data->>'offer_id' AS offer_id,
           market_code,occurred_at
    FROM active_analytics_event_facts
    WHERE tenant_id=$1 AND event_type='order.provider_assigned' AND traffic_class='production'
      AND market_code IS NOT NULL AND ($4='' OR market_code=$4)
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
          AND c.traffic_class='production' AND c.market_code=o.market_code
          AND c.data->>'order_id'=o.order_id AND c.occurred_at>=o.created_at
    )),
    (SELECT count(*) FROM created_orders o WHERE EXISTS (
        SELECT 1 FROM active_analytics_event_facts c
        WHERE c.tenant_id=o.tenant_id AND c.event_type='order.state_changed'
          AND c.traffic_class='production' AND c.market_code=o.market_code
          AND c.data->>'order_id'=o.order_id AND c.data->>'to'='completed'
          AND c.occurred_at>=o.created_at
    )),
    COALESCE((SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY seconds) FROM assignment_durations),0),
    COALESCE((SELECT percentile_cont(0.9) WITHIN GROUP (ORDER BY seconds) FROM assignment_durations),0),
    (SELECT count(*) FROM active_analytics_event_facts WHERE tenant_id=$1 AND occurred_at >= $2 AND occurred_at < $3
       AND traffic_class='production' AND market_code IS NOT NULL AND ($4='' OR market_code=$4)),
    (SELECT count(*) FROM active_analytics_event_facts WHERE tenant_id=$1 AND occurred_at >= $2 AND occurred_at < $3
       AND (traffic_class IS DISTINCT FROM 'production' OR market_code IS NULL)
       AND ($4='' OR market_code=$4 OR market_code IS NULL))
`,tenantID,from,to,filter.MarketCode).Scan(
        &result.OrdersCreated,&result.OffersCreated,&result.OffersAccepted,
        &result.OrdersCancelled,&result.OrdersCompleted,
        &result.MedianAssignmentSeconds,&result.P90AssignmentSeconds,
        &result.EligibleEvents,&result.ExcludedEvents)
    if err!=nil { return Summary{},err }
    result.DataReady=result.EligibleEvents>0
    if result.OffersCreated>0 { result.OfferAcceptanceRate=float64(result.OffersAccepted)/float64(result.OffersCreated) }
    if result.OrdersCreated>0 {
        result.CancellationRate=float64(result.OrdersCancelled)/float64(result.OrdersCreated)
        result.CompletionRate=float64(result.OrdersCompleted)/float64(result.OrdersCreated)
    }
    return result,nil
}
