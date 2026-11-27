package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func TestVersionedCancellationPolicyAndIdempotencyPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","004_catalog.sql","005_pricing.sql","007_market_config.sql","026_market_config_audit.sql","008_catalog_localization.sql","015_order_schedule.sql","022_identity_memberships.sql","023_order_tenant_scope.sql","026_market_config_audit.sql","027_service_form_versions.sql","028_order_service_form_answers.sql","016_cancellation_policy.sql","017_provider_availability.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	ids:=make([]string,4)
	for i:=range ids {
		ids[i],err=newID()
		if err!=nil { t.Fatal(err) }
		phone:=fmt.Sprintf("+1993%011d",(time.Now().UnixNano()+int64(i))%100_000_000_000)
		if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,ids[i],phone); err!=nil { t.Fatal(err) }
	}
	customer:=Actor{SubjectID:ids[0],TenantID:"market-om",Role:RoleCustomer}
	policyVersion:="cancel-v1-"+fmt.Sprint(time.Now().UnixNano())
	otherCustomer:=Actor{SubjectID:ids[1],TenantID:"market-om",Role:RoleCustomer}
	operator:=Actor{SubjectID:ids[2],TenantID:"platform",Role:RoleOperator}
	if err=ensureTestOperatorGrants(ctx,pool,operator,ActionMarketConfigure,ActionPolicyManage);err!=nil{t.Fatal(err)}
	if _,err=store.SaveMarket(ctx,operator,MarketConfig{Code:"OM",DefaultLocale:"en",Currency:"OMR",Timezone:"Asia/Muscat",PolicyVersion:"market-v1",Enabled:true,Locales:[]string{"en","ar"}}); err!=nil {
		t.Fatal(err)
	}
	if _,err=store.SaveCancellationPolicy(ctx,operator,CancellationPolicy{
		MarketCode:"OM",PolicyVersion:policyVersion,ActorRole:RoleCustomer,FromStatus:"requested",
		CutoffMinutes:60,FeeBasisPoints:1000,EffectiveFrom:time.Now().UTC().Add(-time.Hour),
	}); err!=nil { t.Fatal(err) }
	if _,err=store.SaveCancellationPolicy(ctx,customer,CancellationPolicy{
		MarketCode:"OM",PolicyVersion:"cancel-v2",ActorRole:RoleCustomer,FromStatus:"requested",
		CutoffMinutes:30,FeeBasisPoints:0,EffectiveFrom:time.Now().UTC(),
	}); !errors.Is(err,ErrForbidden) { t.Fatalf("customer cannot publish policy: %v",err) }

	category,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("cancel-%d",time.Now().UnixNano()),"Cancellation")
	if err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO market_category_visibility(market_code,category_id,visible) VALUES('OM',$1,true)`,category.ID);err!=nil { t.Fatal(err) }
	menu,err:=store.CreatePriceMenu(ctx,category.ID,"cancellation-service")
	if err!=nil { t.Fatal(err) }
	if _,err=store.CreatePriceVersion(ctx,menu.ID,4500,"OMR"); err!=nil { t.Fatal(err) }
	createOrder:=func(key string,scheduled time.Time) Order {
		order,created,createErr:=store.CreateScheduledOrder(ctx,customer,key,menu.ID,scheduled,"OM","Muscat","Muscat","Al-Khuwair")
		if createErr!=nil || !created { t.Fatalf("create order: %#v created=%v err=%v",order,created,createErr) }
		return order
	}

	professional:=Actor{SubjectID:ids[3],TenantID:customer.TenantID,Role:RoleProfessional}
	provider,err:=store.CreateProviderProfile(ctx,professional,"Cancellation test provider")
	if err!=nil { t.Fatalf("create provider profile: %v",err) }
	bookingOrder:=createOrder("cancel-booking-"+fmt.Sprint(time.Now().UnixNano()),time.Now().UTC().Add(4*time.Hour))
	bookingID,err:=newID()
	if err!=nil { t.Fatal(err) }
	bookingStart:=bookingOrder.ScheduledAt
	if bookingStart==nil { t.Fatal("booking order must have a scheduled time") }
	if _,err=pool.Exec(ctx,`INSERT INTO provider_bookings(id,provider_id,order_id,start_at,end_at,timezone,created_by_identity_id)
		VALUES($1,$2,$3,$4,$5,'Asia/Muscat',$6)`,bookingID,provider.ID,bookingOrder.ID,*bookingStart,bookingStart.Add(time.Hour),professional.SubjectID);err!=nil {
		t.Fatalf("create provider booking: %v",err)
	}
	if _,err=store.CancelOrder(ctx,customer,bookingOrder.ID,"cancel-booking-key","Provider booking cancellation");err!=nil {
		t.Fatalf("cancel order with provider booking: %v",err)
	}
	var bookingStatus string
	if err=pool.QueryRow(ctx,`SELECT status FROM provider_bookings WHERE id=$1`,bookingID).Scan(&bookingStatus);err!=nil || bookingStatus!="cancelled" {
		t.Fatalf("provider booking must be released atomically: status=%q err=%v",bookingStatus,err)
	}
	var bookingEventRaw []byte
	if err=pool.QueryRow(ctx,`SELECT payload FROM outbox_events WHERE aggregate_type='order' AND aggregate_id=$1 AND event_type='provider.booking_cancelled'`,bookingOrder.ID).Scan(&bookingEventRaw);err!=nil {
		t.Fatalf("provider booking cancellation event missing: %v",err)
	}
	var bookingEvent outbox.DomainEvent
	if err=json.Unmarshal(bookingEventRaw,&bookingEvent);err!=nil { t.Fatalf("decode provider booking event: %v",err) }
	if err=outbox.ValidateDomainEvent(bookingEvent);err!=nil || bookingEvent.Type!="provider.booking_cancelled" ||
		bookingEvent.TenantID!=customer.TenantID || bookingEvent.Aggregate.ID!=bookingOrder.ID {
		t.Fatalf("provider booking event must be canonical and tenant-scoped: event=%+v err=%v",bookingEvent,err)
	}
	var bookingData map[string]any
	if err=json.Unmarshal(bookingEvent.Data,&bookingData);err!=nil { t.Fatalf("decode provider booking business data: %v",err) }
	if bookingData["order_id"]!=bookingOrder.ID || bookingData["cancellation_id"]=="" || bookingData["status"]!="cancelled" {
		t.Fatalf("provider booking event must retain legacy business fields: %#v",bookingData)
	}
	earlyOrder:=createOrder("cancel-early-"+fmt.Sprint(time.Now().UnixNano()),time.Now().UTC().Add(2*time.Hour))
	cancelled,err:=store.CancelOrder(ctx,customer,earlyOrder.ID,"cancel-key-early","Customer changed plans")
	if err!=nil { t.Fatalf("cancel before cutoff: %v",err) }
	if cancelled.FeeMinor!=0 || cancelled.RefundMinor!=4500 || cancelled.PolicyVersion!=policyVersion {
		t.Fatalf("early cancellation outcome: %#v",cancelled)
	}
	replay,err:=store.CancelOrder(ctx,customer,earlyOrder.ID,"cancel-key-early","Customer changed plans")
	if err!=nil || replay.ID!=cancelled.ID { t.Fatalf("same-key cancellation replay: %#v %v",replay,err) }
	if _,err=store.CancelOrder(ctx,customer,earlyOrder.ID,"cancel-key-early","different reason"); !errors.Is(err,ErrCancellationIdempotencyConflict) {
		t.Fatalf("changed cancellation payload must fail: %v",err)
	}
	replay,err=store.CancelOrder(ctx,customer,earlyOrder.ID,"new-cancel-key","another retry")
	if err!=nil || replay.ID!=cancelled.ID { t.Fatalf("repeated cancellation with new key must be idempotent: %#v %v",replay,err) }
	if _,err=store.CancelOrder(ctx,otherCustomer,earlyOrder.ID,"other-user-key","unauthorized"); !errors.Is(err,ErrForbidden) {
		t.Fatalf("another customer cannot cancel order: %v",err)
	}
	var status string
	if err=pool.QueryRow(ctx,`SELECT status FROM orders WHERE id=$1`,earlyOrder.ID).Scan(&status); err!=nil || status!="cancelled" {
		t.Fatalf("cancelled order state: %s %v",status,err)
	}
	var cancellationEventRaw []byte
	if err=pool.QueryRow(ctx,`SELECT payload FROM outbox_events WHERE aggregate_type='order' AND aggregate_id=$1 AND event_type='order.cancelled'`,earlyOrder.ID).Scan(&cancellationEventRaw); err!=nil { t.Fatal(err) }
	var cancellationEvent outbox.DomainEvent
	if err=json.Unmarshal(cancellationEventRaw,&cancellationEvent); err!=nil { t.Fatalf("decode cancellation event: %v",err) }
	if err=outbox.ValidateDomainEvent(cancellationEvent); err!=nil || cancellationEvent.TenantID!=customer.TenantID || cancellationEvent.Aggregate.ID!=earlyOrder.ID {
		t.Fatalf("cancellation event must be canonical and tenant-scoped: event=%+v err=%v",cancellationEvent,err)
	}
	var businessData map[string]any
	if err=json.Unmarshal(cancellationEvent.Data,&businessData); err!=nil { t.Fatalf("decode cancellation business data: %v",err) }
	if businessData["order_id"]!=earlyOrder.ID || businessData["from"]!="requested" ||
		businessData["to"]!="cancelled" || businessData["cancellation_id"]!=cancelled.ID {
		t.Fatalf("canonical envelope must retain the legacy cancellation business fields: %#v",businessData)
	}
	var cancellationEventCount int
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM outbox_events WHERE aggregate_type='order' AND aggregate_id=$1 AND event_type='order.cancelled'`,earlyOrder.ID).Scan(&cancellationEventCount); err!=nil { t.Fatal(err) }
	if cancellationEventCount!=1 { t.Fatalf("idempotent cancellation must append exactly one event, got %d",cancellationEventCount) }

	lateOrder:=createOrder("cancel-late-"+fmt.Sprint(time.Now().UnixNano()),time.Now().UTC().Add(45*time.Minute))
	late,err:=store.CancelOrder(ctx,customer,lateOrder.ID,"cancel-key-late","Late cancellation")
	if err!=nil { t.Fatalf("cancel inside cutoff: %v",err) }
	if late.FeeMinor!=450 || late.RefundMinor!=4050 { t.Fatalf("late cancellation fee: %#v",late) }

	blockedOrder:=createOrder("cancel-bypass-"+fmt.Sprint(time.Now().UnixNano()),time.Now().UTC().Add(2*time.Hour))
	if _,err=store.TransitionOrder(ctx,blockedOrder.ID,"cancelled"); err==nil {
		t.Fatal("direct state transition must not bypass versioned cancellation policy")
	}
	if _,err=pool.Exec(ctx,`UPDATE order_cancellations SET fee_minor=1 WHERE order_id=$1`,earlyOrder.ID); err==nil {
		t.Fatal("cancellation outcomes must be immutable")
	}
	if _,err=pool.Exec(ctx,`UPDATE cancellation_policies SET fee_basis_points=0 WHERE market_code='OM'`); err==nil {
		t.Fatal("cancellation policy versions must be immutable")
	}
}
