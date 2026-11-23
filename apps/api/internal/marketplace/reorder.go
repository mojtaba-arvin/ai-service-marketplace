package marketplace

import (
    "context"
    "errors"
    "strings"
    "time"

    "github.com/jackc/pgx/v5"
)

func (s *Store) ReorderScheduledOrder(ctx context.Context, actor Actor, originalOrderID, idempotencyKey string, scheduledAt time.Time, marketCode string, answersJSON []byte) (Order, bool, error) {
    if actor.Role != RoleCustomer || actor.SubjectID == "" || actor.TenantID == "" || strings.TrimSpace(originalOrderID) == "" {
        return Order{}, false, ErrForbidden
    }
    if err := Authorize(actor, Resource{TenantID: actor.TenantID, OwnerID: actor.SubjectID}, ActionOrderCreate); err != nil {
        return Order{}, false, err
    }
    idempotencyKey = strings.TrimSpace(idempotencyKey)
    marketCode = strings.ToUpper(strings.TrimSpace(marketCode))
    scheduledAt = scheduledAt.UTC().Truncate(time.Microsecond)
    now := time.Now().UTC()
    if idempotencyKey == "" || len(idempotencyKey) > 160 || scheduledAt.Before(now.Add(30*time.Minute)) || scheduledAt.After(now.Add(90*24*time.Hour)) {
        return Order{}, false, ErrInvalidOrderRequest
    }
    scopedKey := "reorder:" + originalOrderID + ":" + idempotencyKey
    if len(scopedKey) > 200 { return Order{}, false, ErrInvalidOrderRequest }

    var original Order
    var status string
    var priceVersionID *string
    var originalAnswers *string
    err := s.DB.QueryRow(ctx, "SELECT o.id,o.customer_id,o.amount_minor,o.currency,o.status,o.price_version_id,o.scheduled_at,o.country_code,o.region_code,o.city_code,o.area_code,a.answers::text FROM orders o LEFT JOIN order_service_form_answers a ON a.order_id=o.id WHERE o.id=$1 AND o.customer_id=$2 AND o.tenant_id=$3", originalOrderID, actor.SubjectID, actor.TenantID).Scan(&original.ID,&original.CustomerID,&original.AmountMinor,&original.Currency,&status,&priceVersionID,&original.ScheduledAt,&original.CountryCode,&original.RegionCode,&original.CityCode,&original.AreaCode,&originalAnswers)
    if errors.Is(err, pgx.ErrNoRows) { return Order{}, false, ErrOrderNotFound }
    if err != nil { return Order{}, false, err }
    if status != "completed" { return Order{}, false, ErrInvalidTransition }
    if priceVersionID == nil || original.CountryCode == nil || original.RegionCode == nil || original.CityCode == nil || original.AreaCode == nil {
        return Order{}, false, ErrInvalidOrderRequest
    }
    country, region, city, area := *original.CountryCode, *original.RegionCode, *original.CityCode, *original.AreaCode
    if marketCode == "" || country != marketCode { return Order{}, false, ErrMarketUnavailable }
    if !validOrderArea(country, region, city, area) { return Order{}, false, ErrInvalidOrderRequest }

    var menuID string
    err = s.DB.QueryRow(ctx, "SELECT menu_id FROM service_price_versions WHERE id=$1", *priceVersionID).Scan(&menuID)
    if errors.Is(err, pgx.ErrNoRows) { return Order{}, false, ErrPriceUnavailable }
    if err != nil { return Order{}, false, err }
    var categoryID string
    err = s.DB.QueryRow(ctx, "SELECT category_id FROM service_price_menus WHERE id=$1", menuID).Scan(&categoryID)
    if errors.Is(err, pgx.ErrNoRows) { return Order{}, false, ErrPriceMenuNotFound }
    if err != nil { return Order{}, false, err }

    canonicalAnswers := []byte(nil)
    if len(answersJSON) > 0 {
        canonicalAnswers, err = canonicalFormAnswers(answersJSON)
        if err != nil { return Order{}, false, ErrServiceFormAnswersInvalid }
    } else if originalAnswers != nil {
        canonicalAnswers, err = canonicalFormAnswers([]byte(*originalAnswers))
        if err != nil { return Order{}, false, ErrServiceFormAnswersInvalid }
    }

    // Idempotent retries must return the original new order even if availability
    // has changed since the first request. Scope the key to its source order.
    var prior Order
    var priorMenuID string
    var priorAnswers *string
    err = s.DB.QueryRow(ctx, "SELECT o.id,o.customer_id,o.amount_minor,o.currency,o.status,o.price_version_id,o.scheduled_at,o.country_code,o.region_code,o.city_code,o.area_code,v.menu_id,a.answers::text FROM orders o JOIN service_price_versions v ON v.id=o.price_version_id LEFT JOIN order_service_form_answers a ON a.order_id=o.id WHERE o.customer_id=$1 AND o.idempotency_key=$2 AND o.tenant_id=$3", actor.SubjectID, scopedKey, actor.TenantID).Scan(&prior.ID,&prior.CustomerID,&prior.AmountMinor,&prior.Currency,&prior.Status,&prior.PriceVersionID,&prior.ScheduledAt,&prior.CountryCode,&prior.RegionCode,&prior.CityCode,&prior.AreaCode,&priorMenuID,&priorAnswers)
    if err == nil {
        if !sameScheduledOrder(prior, menuID, scheduledAt, country, region, city, area, priorMenuID) || !sameFormAnswers(priorAnswers, canonicalAnswers) {
            return Order{}, false, ErrOrderIdempotencyConflict
        }
        return prior, false, nil
    }
    if !errors.Is(err, pgx.ErrNoRows) { return Order{}, false, err }

    candidates, err := s.FindDispatchCandidates(ctx, actor, categoryID, country, region, city, area, scheduledAt.Add(-30*time.Minute), scheduledAt.Add(30*time.Minute))
    if err != nil { return Order{}, false, err }
    if len(candidates) == 0 { return Order{}, false, ErrProviderUnavailable }

    // The normal creation path revalidates the active catalog, current price,
    // market visibility and active published form inside its write transaction.
    return s.CreateScheduledOrderWithAnswers(ctx, actor, scopedKey, menuID, scheduledAt, country, region, city, area, canonicalAnswers)
}
