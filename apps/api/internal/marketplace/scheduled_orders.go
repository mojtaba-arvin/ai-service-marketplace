package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func validOrderArea(country,region,city,area string) bool {
	if len(country)!=2 || country[0]<'A' || country[0]>'Z' || country[1]<'A' || country[1]>'Z' { return false }
	return region!="" && city!="" && area!="" && len(region)<=100 && len(city)<=100 && len(area)<=100
}

func sameScheduledOrder(existing Order,menuID string,scheduledAt time.Time,country,region,city,area string,existingMenuID string) bool {
	return existingMenuID==menuID && existing.ScheduledAt!=nil && existing.ScheduledAt.Equal(scheduledAt) &&
		existing.CountryCode!=nil && *existing.CountryCode==country &&
		existing.RegionCode!=nil && *existing.RegionCode==region &&
		existing.CityCode!=nil && *existing.CityCode==city &&
		existing.AreaCode!=nil && *existing.AreaCode==area
}

// CreateScheduledOrder creates a customer-owned order with an immutable price
// version and service-area/schedule snapshot. Replays return the original only
// when every request dimension matches the original idempotency payload.
func (s *Store) CreateScheduledOrder(ctx context.Context,actor Actor,idempotencyKey,menuID string,
	scheduledAt time.Time,country,region,city,area string) (Order,bool,error) {
	if actor.Role!=RoleCustomer || actor.SubjectID=="" || actor.TenantID=="" {
		return Order{},false,ErrForbidden
	}
	if err:=Authorize(actor,Resource{TenantID:actor.TenantID},ActionOrderCreate); err!=nil {
		return Order{},false,err
	}
	idempotencyKey=strings.TrimSpace(idempotencyKey)
	country=strings.ToUpper(strings.TrimSpace(country))
	region=strings.TrimSpace(region); city=strings.TrimSpace(city); area=strings.TrimSpace(area)
	now:=time.Now().UTC()
	scheduledAt=scheduledAt.UTC()
	if idempotencyKey=="" || len(idempotencyKey)>200 || menuID=="" ||
		scheduledAt.Before(now.Add(30*time.Minute)) || scheduledAt.After(now.Add(90*24*time.Hour)) ||
		!validOrderArea(country,region,city,area) {
		return Order{},false,ErrInvalidCategory
	}

	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return Order{},false,err }
	defer tx.Rollback(ctx)

	var prior Order
	var priorMenuID string
	err=tx.QueryRow(ctx,`
		SELECT o.id,o.customer_id,o.amount_minor,o.currency,o.status,o.price_version_id,v.menu_id,
			o.scheduled_at,o.country_code,o.region_code,o.city_code,o.area_code
		FROM orders o JOIN service_price_versions v ON v.id=o.price_version_id
		WHERE o.customer_id=$1 AND o.idempotency_key=$2
	`,actor.SubjectID,idempotencyKey).Scan(&prior.ID,&prior.CustomerID,&prior.AmountMinor,&prior.Currency,
		&prior.Status,&prior.PriceVersionID,&priorMenuID,&prior.ScheduledAt,&prior.CountryCode,
		&prior.RegionCode,&prior.CityCode,&prior.AreaCode)
	if err==nil {
		if !sameScheduledOrder(prior,menuID,scheduledAt,country,region,city,area,priorMenuID) {
			return Order{},false,ErrOrderIdempotencyConflict
		}
		if err=tx.Commit(ctx); err!=nil { return Order{},false,err }
		return prior,false,nil
	}
	if !errors.Is(err,pgx.ErrNoRows) { return Order{},false,err }

	var version PriceVersion
	err=tx.QueryRow(ctx,`
		SELECT v.id,v.menu_id,v.version_no,v.amount_minor,v.currency,v.created_at
		FROM service_price_menus m JOIN service_price_versions v ON v.id=m.active_version_id
		WHERE m.id=$1 FOR SHARE OF m
	`,menuID).Scan(&version.ID,&version.MenuID,&version.VersionNo,&version.AmountMinor,&version.Currency,&version.CreatedAt)
	if errors.Is(err,pgx.ErrNoRows) { return Order{},false,ErrPriceMenuNotFound }
	if err!=nil { return Order{},false,err }
	var categoryID string
	if err=tx.QueryRow(ctx,`SELECT category_id FROM service_price_menus WHERE id=$1`,menuID).Scan(&categoryID); err!=nil {
		return Order{},false,err
	}
	enabled,err:=categoryAvailableForOrder(ctx,tx,categoryID)
	if err!=nil { return Order{},false,err }
	if !enabled { return Order{},false,ErrPriceUnavailable }

	id,err:=newID()
	if err!=nil { return Order{},false,err }
	var createdID string
	err=tx.QueryRow(ctx,`
		INSERT INTO orders(id,customer_id,idempotency_key,amount_minor,currency,status,price_version_id,
			scheduled_at,country_code,region_code,city_code,area_code)
		VALUES($1,$2,$3,$4,$5,'requested',$6,$7,$8,$9,$10,$11)
		ON CONFLICT(customer_id,idempotency_key) DO NOTHING
		RETURNING id
	`,id,actor.SubjectID,idempotencyKey,version.AmountMinor,version.Currency,version.ID,
		scheduledAt,country,region,city,area).Scan(&createdID)
	if errors.Is(err,pgx.ErrNoRows) {
		var existing Order
		var existingMenuID string
		err=tx.QueryRow(ctx,`
			SELECT o.id,o.customer_id,o.amount_minor,o.currency,o.status,o.price_version_id,v.menu_id,
				o.scheduled_at,o.country_code,o.region_code,o.city_code,o.area_code
			FROM orders o JOIN service_price_versions v ON v.id=o.price_version_id
			WHERE o.customer_id=$1 AND o.idempotency_key=$2
		`,actor.SubjectID,idempotencyKey).Scan(&existing.ID,&existing.CustomerID,&existing.AmountMinor,&existing.Currency,
			&existing.Status,&existing.PriceVersionID,&existingMenuID,&existing.ScheduledAt,&existing.CountryCode,
			&existing.RegionCode,&existing.CityCode,&existing.AreaCode)
		if err!=nil { return Order{},false,err }
		if !sameScheduledOrder(existing,menuID,scheduledAt,country,region,city,area,existingMenuID) {
			return Order{},false,ErrOrderIdempotencyConflict
		}
		if err=tx.Commit(ctx); err!=nil { return Order{},false,err }
		return existing,false,nil
	}
	if err!=nil { return Order{},false,err }

	_,err=tx.Exec(ctx,`
		INSERT INTO ledger_journals(id,idempotency_key,currency,description)
		VALUES($1,$2,$3,'walking-skeleton simulated order hold')
	`,id,"order:"+id,version.Currency)
	if err!=nil { return Order{},false,fmt.Errorf("create test ledger journal: %w",err) }
	_,err=tx.Exec(ctx,`
		INSERT INTO ledger_entries(journal_id,account_code,side,amount_minor)
		VALUES($1,'customer_funds','debit',$2),($1,'provider_payable','credit',$2)
	`,id,version.AmountMinor)
	if err!=nil { return Order{},false,fmt.Errorf("post balanced test ledger: %w",err) }
	payload,_:=json.Marshal(map[string]any{
		"order_id":id,"status":"requested","price_version_id":version.ID,
		"scheduled_at":scheduledAt,"country_code":country,"region_code":region,"city_code":city,"area_code":area,
	})
	if _,err=tx.Exec(ctx,`INSERT INTO outbox_events(aggregate_type,aggregate_id,event_type,payload) VALUES('order',$1,'order.created',$2::jsonb)`,id,payload); err!=nil {
		return Order{},false,err
	}
	if err=tx.Commit(ctx); err!=nil { return Order{},false,err }
	return Order{ID:id,CustomerID:actor.SubjectID,AmountMinor:version.AmountMinor,Currency:version.Currency,
		Status:"requested",PriceVersionID:&version.ID,ScheduledAt:&scheduledAt,
		CountryCode:&country,RegionCode:&region,CityCode:&city,AreaCode:&area},true,nil
}
