package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCustomerScheduledOrderAggregatePostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","004_catalog.sql","005_pricing.sql","007_market_config.sql","008_catalog_localization.sql","015_order_schedule.sql","022_identity_memberships.sql","023_order_tenant_scope.sql","026_market_config_audit.sql","027_service_form_versions.sql","028_order_service_form_answers.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	identityID,err:=newID()
	if err!=nil { t.Fatal(err) }
	phone:=fmt.Sprintf("+1994%011d",time.Now().UnixNano()%100_000_000_000)
	if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,identityID,phone); err!=nil { t.Fatal(err) }
	customer:=Actor{SubjectID:identityID,TenantID:"market-om",Role:RoleCustomer}
	category,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("cleaning-%d",time.Now().UnixNano()),"Cleaning")
	if err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO markets(code,default_locale,currency,timezone,policy_version,enabled) VALUES('OM','en','OMR','Asia/Muscat','mvp-1',true) ON CONFLICT(code) DO UPDATE SET enabled=true`);err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO market_locales(market_code,locale,is_default) VALUES('OM','en',true) ON CONFLICT(market_code,locale) DO NOTHING`);err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO market_category_visibility(market_code,category_id,visible) VALUES('OM',$1,true)`,category.ID);err!=nil { t.Fatal(err) }
	menu,err:=store.CreatePriceMenu(ctx,category.ID,"scheduled-cleaning")
	if err!=nil { t.Fatal(err) }
	if _,err=store.CreatePriceVersion(ctx,menu.ID,4500,"OMR"); err!=nil { t.Fatal(err) }
	scheduled:=time.Now().UTC().Add(2*time.Hour).Truncate(time.Microsecond)
	key:="scheduled-order-"+fmt.Sprint(time.Now().UnixNano())
	order,created,err:=store.CreateScheduledOrder(ctx,customer,key,menu.ID,scheduled,"om","Muscat","Muscat","Al-Khuwair")
	if err!=nil || !created { t.Fatalf("create scheduled order: %#v created=%v err=%v",order,created,err) }
	var simulatedJournalCount int
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM ledger_journals WHERE id=$1`,order.ID).Scan(&simulatedJournalCount);err!=nil { t.Fatal(err) }
	if simulatedJournalCount!=0 { t.Fatalf("booking creation must not create a simulated payment journal, got %d",simulatedJournalCount) }
	if order.CustomerID!=identityID || order.AmountMinor!=4500 || order.Currency!="OMR" ||
		order.ScheduledAt==nil || !order.ScheduledAt.Equal(scheduled) ||
		order.CountryCode==nil || *order.CountryCode!="OM" ||
		order.RegionCode==nil || *order.RegionCode!="Muscat" ||
		order.CityCode==nil || *order.CityCode!="Muscat" ||
		order.AreaCode==nil || *order.AreaCode!="Al-Khuwair" ||
		order.PriceVersionID==nil {
		t.Fatalf("scheduled order snapshot incomplete: %#v",order)
	}
	replay,created,err:=store.CreateScheduledOrder(ctx,customer,key,menu.ID,scheduled,"OM","Muscat","Muscat","Al-Khuwair")
	if err!=nil || created || replay.ID!=order.ID { t.Fatalf("exact replay must return original order: %#v created=%v err=%v",replay,created,err) }
	if _,_,err=store.CreateScheduledOrder(ctx,customer,key,menu.ID,scheduled.Add(time.Hour),"OM","Muscat","Muscat","Al-Khuwair"); !errors.Is(err,ErrOrderIdempotencyConflict) {
		t.Fatalf("changed schedule under same key must fail: %v",err)
	}
	if _,_,err=store.CreateScheduledOrder(ctx,customer,key,menu.ID,scheduled,"OM","Muscat","Seeb","Al-Khuwair"); !errors.Is(err,ErrOrderIdempotencyConflict) {
		t.Fatalf("changed area under same key must fail: %v",err)
	}


	// Race two different payloads under the same tenant/customer idempotency key.
	// Exactly one request may create the order; the loser must observe a payload
	// conflict rather than creating a second order or outbox event.
	raceKey := "scheduled-order-race-" + fmt.Sprint(time.Now().UnixNano())
	type createResult struct {
		order Order
		created bool
		err error
	}
	startRace := make(chan struct{})
	results := make(chan createResult, 2)
	var raceWG sync.WaitGroup
	for _, area := range []string{"Al-Khuwair", "Ghubrah"} {
		area := area
		raceWG.Add(1)
		go func() {
			defer raceWG.Done()
			<-startRace
			createdOrder, wasCreated, createErr := store.CreateScheduledOrder(ctx, customer, raceKey, menu.ID, scheduled.Add(3*time.Hour), "OM", "Muscat", "Muscat", area)
			results <- createResult{order: createdOrder, created: wasCreated, err: createErr}
		}()
	}
	close(startRace)
	raceWG.Wait()
	close(results)
	createdCount, conflictCount := 0, 0
	var racedOrderID string
	for result := range results {
		if result.err == nil && result.created {
			createdCount++
			racedOrderID = result.order.ID
			continue
		}
		if errors.Is(result.err, ErrOrderIdempotencyConflict) {
			conflictCount++
			continue
		}
		t.Fatalf("unexpected concurrent idempotency result: created=%v order=%+v err=%v", result.created, result.order, result.err)
	}
	if createdCount != 1 || conflictCount != 1 || racedOrderID == "" {
		t.Fatalf("concurrent changed-payload requests must have one winner and one conflict: created=%d conflicts=%d id=%q", createdCount, conflictCount, racedOrderID)
	}
	var racedOrders, racedEvents int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE tenant_id=$1 AND customer_id=$2 AND idempotency_key=$3`, customer.TenantID, customer.SubjectID, raceKey).Scan(&racedOrders); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_type='order' AND aggregate_id=$1 AND event_type='order.created'`, racedOrderID).Scan(&racedEvents); err != nil {
		t.Fatal(err)
	}
	if racedOrders != 1 || racedEvents != 1 {
		t.Fatalf("idempotency race must persist one order and one creation event: orders=%d events=%d", racedOrders, racedEvents)
	}

	professional:=Actor{SubjectID:identityID,TenantID:"market-om",Role:RoleProfessional}
	if _,_,err=store.CreateScheduledOrder(ctx,professional,"not-customer",menu.ID,scheduled,"OM","Muscat","Muscat","Al-Khuwair"); !errors.Is(err,ErrForbidden) {
		t.Fatalf("non-customer cannot create customer order: %v",err)
	}
	if _,_,err=store.CreateScheduledOrder(ctx,customer,"too-soon",menu.ID,time.Now().UTC().Add(5*time.Minute),"OM","Muscat","Muscat","Al-Khuwair"); !errors.Is(err,ErrInvalidOrderRequest) {
		t.Fatalf("schedule inside minimum lead time must fail: %v",err)
	}
	// Select a market code absent from the shared PostgreSQL fixture. Other
	// integration tests may seed US (or another familiar market) as enabled.
	unknownMarket := ""
	for _, candidate := range []string{"ZZ","QZ","ZX","QQ","XQ","XZ","ZQ","QX"} {
		var exists bool
		if err=pool.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM markets WHERE code=$1)`,candidate).Scan(&exists);err!=nil { t.Fatal(err) }
		if !exists { unknownMarket=candidate;break }
	}
	if unknownMarket=="" { t.Fatal("could not find an unconfigured market code for negative test") }
	if _,_,err=store.CreateScheduledOrder(ctx,customer,"wrong-market",menu.ID,scheduled,unknownMarket,"California","San Francisco","Downtown"); !errors.Is(err,ErrMarketUnavailable) {
		t.Fatalf("orders outside the enabled launch market must fail: %v",err)
	}
	if _,err=pool.Exec(ctx,`UPDATE market_category_visibility SET visible=false WHERE market_code='OM' AND category_id=$1`,category.ID);err!=nil { t.Fatal(err) }
	if _,_,err=store.CreateScheduledOrder(ctx,customer,"hidden-category",menu.ID,scheduled,"OM","Muscat","Muscat","Al-Khuwair"); !errors.Is(err,ErrPriceUnavailable) {
		t.Fatalf("hidden market category must reject new orders: %v",err)
	}
	if _,err=pool.Exec(ctx,`UPDATE market_category_visibility SET visible=true WHERE market_code='OM' AND category_id=$1`,category.ID);err!=nil { t.Fatal(err) }
	if err=store.SetCategoryEnabled(ctx,category.ID,false); err!=nil { t.Fatal(err) }
	replay,created,err=store.CreateScheduledOrder(ctx,customer,key,menu.ID,scheduled,"OM","Muscat","Muscat","Al-Khuwair")
	if err!=nil || created || replay.ID!=order.ID { t.Fatalf("idempotent replay must survive catalog disable: %#v %v",replay,err) }
	if _,_,err=store.CreateScheduledOrder(ctx,customer,"new-after-disable",menu.ID,scheduled,"OM","Muscat","Muscat","Al-Khuwair"); !errors.Is(err,ErrPriceUnavailable) {
		t.Fatalf("disabled category must reject new orders: %v",err)
	}
	transitioned,err:=store.TransitionOrder(ctx,order.ID,"accepted")
	if err!=nil || transitioned.ScheduledAt==nil || !transitioned.ScheduledAt.Equal(scheduled) {
		t.Fatalf("state transition must preserve order schedule: %#v %v",transitioned,err)
	}
}
