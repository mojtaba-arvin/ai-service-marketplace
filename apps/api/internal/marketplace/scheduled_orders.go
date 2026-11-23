package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/serviceforms"
)

var (
 ErrInvalidOrderRequest = errors.New("invalid scheduled order request")
 ErrServiceFormAnswersRequired = errors.New("published service form answers are required")
 ErrServiceFormAnswersInvalid = errors.New("service form answers are invalid")
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
 return s.CreateScheduledOrderWithAnswers(ctx,actor,idempotencyKey,menuID,scheduledAt,country,region,city,area,nil)
}

// CreateScheduledOrderWithAnswers validates the active market questionnaire and
// atomically stores its exact version and canonical answer snapshot with the order.
func (s *Store) CreateScheduledOrderWithAnswers(ctx context.Context,actor Actor,idempotencyKey,menuID string,
 scheduledAt time.Time,country,region,city,area string,answersJSON []byte) (Order,bool,error) {
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
	scheduledAt=scheduledAt.UTC().Truncate(time.Microsecond)
	if idempotencyKey=="" || len(idempotencyKey)>200 || menuID=="" ||
		scheduledAt.Before(now.Add(30*time.Minute)) || scheduledAt.After(now.Add(90*24*time.Hour)) ||
		!validOrderArea(country,region,city,area) || len(answersJSON)>16*1024 {
		return Order{},false,ErrInvalidOrderRequest
	}
 canonicalAnswers,canonicalErr:=canonicalFormAnswers(answersJSON)
 if canonicalErr!=nil{return Order{},false,ErrServiceFormAnswersInvalid}

	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return Order{},false,err }
	defer tx.Rollback(ctx)

	var prior Order
	var priorMenuID string
 var priorAnswers *string
	err=tx.QueryRow(ctx,`
		SELECT o.id,o.customer_id,o.amount_minor,o.currency,o.status,o.price_version_id,v.menu_id,
			o.scheduled_at,o.country_code,o.region_code,o.city_code,o.area_code,a.answers::text
		FROM orders o JOIN service_price_versions v ON v.id=o.price_version_id
 LEFT JOIN order_service_form_answers a ON a.order_id=o.id
		WHERE o.customer_id=$1 AND o.idempotency_key=$2 AND o.tenant_id=$3
	`,actor.SubjectID,idempotencyKey,actor.TenantID).Scan(&prior.ID,&prior.CustomerID,&prior.AmountMinor,&prior.Currency,
		&prior.Status,&prior.PriceVersionID,&priorMenuID,&prior.ScheduledAt,&prior.CountryCode,
		&prior.RegionCode,&prior.CityCode,&prior.AreaCode,&priorAnswers)
	if err==nil {
		if !sameScheduledOrder(prior,menuID,scheduledAt,country,region,city,area,priorMenuID) || !sameFormAnswers(priorAnswers,canonicalAnswers) {
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

	var marketCurrency string
	err=tx.QueryRow(ctx,`SELECT currency FROM markets WHERE code=$1 AND enabled=true`,country).Scan(&marketCurrency)
	if errors.Is(err,pgx.ErrNoRows) { return Order{},false,ErrMarketUnavailable }
	if err!=nil { return Order{},false,err }
	if marketCurrency!=version.Currency { return Order{},false,ErrPriceUnavailable }
	var marketVisible bool
	err=tx.QueryRow(ctx,`
		SELECT EXISTS(
			SELECT 1 FROM market_category_visibility
			WHERE market_code=$1 AND category_id=$2 AND visible=true
		)
	`,country,categoryID).Scan(&marketVisible)
	if err!=nil { return Order{},false,err }
	if !marketVisible { return Order{},false,ErrPriceUnavailable }

 // Resolve the active questionnaire only for a new order; replays above retain
 // their original version even if a newer form has since been published.
 var formID *string
 var formSchema []byte
 var formVersion int
 var serviceCode,locale string
 err=tx.QueryRow(ctx,`
  SELECT f.id,f.schema,f.version_no,m.service_code,f.locale
  FROM markets mk JOIN service_form_versions f ON f.market_code=mk.code AND f.locale=mk.default_locale
   AND f.menu_id=$1 AND f.status='published'
  JOIN service_price_menus m ON m.id=f.menu_id
  WHERE mk.code=$2 AND mk.enabled=true
 `,menuID,country).Scan(&formID,&formSchema,&formVersion,&serviceCode,&locale)
 if errors.Is(err,pgx.ErrNoRows) {
  if len(canonicalAnswers)>0{return Order{},false,ErrServiceFormAnswersInvalid}
  err=nil
 } else if err!=nil { return Order{},false,err
 } else {
  if len(canonicalAnswers)==0{return Order{},false,ErrServiceFormAnswersRequired}
  definition,decodeErr:=decodeServiceFormSchema(formSchema,serviceCode,locale,formVersion)
  if decodeErr!=nil{return Order{},false,fmt.Errorf("%w: published schema is invalid",ErrServiceFormAnswersInvalid)}
  if validateErr:=serviceforms.ValidateAnswers(definition,canonicalAnswers);validateErr!=nil{return Order{},false,fmt.Errorf("%w: %v",ErrServiceFormAnswersInvalid,validateErr)}
 }
	id,err:=newID()
	if err!=nil { return Order{},false,err }
	var createdID string
	err=tx.QueryRow(ctx,`
		INSERT INTO orders(id,customer_id,idempotency_key,amount_minor,currency,status,price_version_id,
			scheduled_at,country_code,region_code,city_code,area_code,tenant_id)
		VALUES($1,$2,$3,$4,$5,'requested',$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(customer_id,idempotency_key) DO NOTHING
		RETURNING id
	`,id,actor.SubjectID,idempotencyKey,version.AmountMinor,version.Currency,version.ID,
		scheduledAt,country,region,city,area,actor.TenantID).Scan(&createdID)
	if errors.Is(err,pgx.ErrNoRows) {
		var existing Order
		var existingMenuID string
		err=tx.QueryRow(ctx,`
			SELECT o.id,o.customer_id,o.amount_minor,o.currency,o.status,o.price_version_id,v.menu_id,
				o.scheduled_at,o.country_code,o.region_code,o.city_code,o.area_code
			FROM orders o JOIN service_price_versions v ON v.id=o.price_version_id
			WHERE o.customer_id=$1 AND o.idempotency_key=$2 AND o.tenant_id=$3
		`,actor.SubjectID,idempotencyKey,actor.TenantID).Scan(&existing.ID,&existing.CustomerID,&existing.AmountMinor,&existing.Currency,
			&existing.Status,&existing.PriceVersionID,&existingMenuID,&existing.ScheduledAt,&existing.CountryCode,
			&existing.RegionCode,&existing.CityCode,&existing.AreaCode)
		if errors.Is(err,pgx.ErrNoRows) { return Order{},false,ErrOrderIdempotencyConflict }
		if err!=nil { return Order{},false,err }
		if !sameScheduledOrder(existing,menuID,scheduledAt,country,region,city,area,existingMenuID) {
			return Order{},false,ErrOrderIdempotencyConflict
		}
		if err=tx.Commit(ctx); err!=nil { return Order{},false,err }
		return existing,false,nil
	}
	if err!=nil { return Order{},false,err }
 if formID!=nil {
  if _,err=tx.Exec(ctx,`INSERT INTO order_service_form_answers(order_id,form_version_id,answers,submitted_by) VALUES($1,$2,$3::jsonb,$4)`,id,*formID,canonicalAnswers,actor.SubjectID);err!=nil{return Order{},false,err}
 }

	// Booking creation does not represent a payment authorization or funds hold.
	// Financial postings are created only by an explicit payment workflow.
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


// canonicalFormAnswers normalizes JSON key ordering for semantic idempotency.
func canonicalFormAnswers(raw []byte)([]byte,error){
 if len(raw)==0{return nil,nil};var value any;dec:=json.NewDecoder(strings.NewReader(string(raw)));dec.UseNumber();if err:=dec.Decode(&value);err!=nil{return nil,err};if err:=dec.Decode(new(any));!errors.Is(err,io.EOF){return nil,errors.New("trailing JSON")};return json.Marshal(value)
}
func sameFormAnswers(stored *string,submitted []byte)bool{if stored==nil{return len(submitted)==0};return string(submitted)==*stored}
