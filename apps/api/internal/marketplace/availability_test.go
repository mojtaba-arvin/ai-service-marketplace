package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderAvailabilityAndNonOverlappingBookingsPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","004_catalog.sql","005_pricing.sql","006_provider_eligibility.sql","007_market_config.sql","008_catalog_localization.sql","015_order_schedule.sql","022_identity_memberships.sql","023_order_tenant_scope.sql","016_cancellation_policy.sql","017_provider_availability.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	ids:=make([]string,3)
	for i:=range ids {
		ids[i],err=newID()
		if err!=nil { t.Fatal(err) }
		phone:=fmt.Sprintf("+1991%011d",(time.Now().UnixNano()+int64(i))%100_000_000_000)
		if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,ids[i],phone); err!=nil { t.Fatal(err) }
	}
	customer:=Actor{SubjectID:ids[0],TenantID:"market-dispatch",Role:RoleCustomer}
	professional:=Actor{SubjectID:ids[1],TenantID:"market-dispatch",Role:RoleProfessional}
	operator:=Actor{SubjectID:ids[2],TenantID:"market-dispatch",Role:RoleOperator}
	if _,err=store.SaveMarket(ctx,operator,MarketConfig{Code:"OM",DefaultLocale:"en",Currency:"OMR",Timezone:"Asia/Muscat",PolicyVersion:"dispatch-market-v1",Enabled:true,Locales:[]string{"en","ar"}}); err!=nil { t.Fatal(err) }
	policyVersion:="dispatch-cancel-"+fmt.Sprint(time.Now().UnixNano())
	if _,err=store.SaveCancellationPolicy(ctx,operator,CancellationPolicy{MarketCode:"OM",PolicyVersion:policyVersion,ActorRole:RoleCustomer,FromStatus:"requested",CutoffMinutes:60,FeeBasisPoints:1000,EffectiveFrom:time.Now().UTC().Add(-time.Hour)}); err!=nil { t.Fatal(err) }
	category,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("availability-%d",time.Now().UnixNano()),"Availability")
	if err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO market_category_visibility(market_code,category_id,visible) VALUES('OM',$1,true)`,category.ID);err!=nil { t.Fatal(err) }
	menu,err:=store.CreatePriceMenu(ctx,category.ID,"availability-service")
	if err!=nil { t.Fatal(err) }
	if _,err=store.CreatePriceVersion(ctx,menu.ID,3000,"OMR"); err!=nil { t.Fatal(err) }
	provider,err:=store.CreateProviderProfile(ctx,professional,"Verified service professional")
	if err!=nil { t.Fatal(err) }
	if err=store.AddProviderSkill(ctx,professional,provider.ID,category.ID); err!=nil { t.Fatal(err) }
	if err=store.AddProviderServiceArea(ctx,professional,provider.ID,"OM","Muscat","Muscat","Al-Khuwair"); err!=nil { t.Fatal(err) }
	if err=store.VerifyProviderProfile(ctx,operator,provider.ID); err!=nil { t.Fatal(err) }
	if err=store.VerifyProviderSkill(ctx,operator,provider.ID,category.ID); err!=nil { t.Fatal(err) }
	if err=store.SetProviderActive(ctx,operator,provider.ID,true); err!=nil { t.Fatal(err) }

	loc,err:=time.LoadLocation("Asia/Muscat")
	if err!=nil { t.Fatal(err) }
	start:=time.Now().In(loc).Add(2*time.Hour).Truncate(time.Minute)
	end:=start.Add(3*time.Hour)
	window,err:=store.SetProviderAvailability(ctx,professional,provider.ID,"available",start,end,loc.String())
	if err!=nil { t.Fatalf("set availability: %v",err) }
	if window.Timezone!="Asia/Muscat" || !window.StartAt.Equal(start.UTC()) { t.Fatalf("availability timezone snapshot: %#v",window) }
	if _,err=store.SetProviderAvailability(ctx,professional,provider.ID,"available",start.Add(time.Hour),end.Add(time.Hour),loc.String()); !errors.Is(err,ErrAvailabilityOverlap) {
		t.Fatalf("overlapping available windows must fail: %v",err)
	}
	if _,err=store.SetProviderAvailability(ctx,Actor{SubjectID:ids[2],TenantID:"market-dispatch",Role:RoleProfessional},provider.ID,"available",start,end,loc.String()); !errors.Is(err,ErrForbidden) {
		t.Fatalf("another professional cannot edit provider calendar: %v",err)
	}
	if _,err=store.SetProviderAvailability(ctx,professional,provider.ID,"available",start,end,"Invalid/Timezone"); !errors.Is(err,ErrInvalidAvailability) {
		t.Fatalf("invalid timezone must fail: %v",err)
	}

	createOrder:=func(key string,scheduled time.Time) Order {
		order,created,createErr:=store.CreateScheduledOrder(ctx,customer,key,menu.ID,scheduled,"OM","Muscat","Muscat","Al-Khuwair")
		if createErr!=nil || !created { t.Fatalf("create scheduled order: %#v %v",order,createErr) }
		return order
	}
	first:=createOrder("booking-first-"+fmt.Sprint(time.Now().UnixNano()),start.Add(30*time.Minute))
	booking,err:=store.CreateProviderBooking(ctx,operator,provider.ID,first.ID,start,start.Add(time.Hour),loc.String())
	if err!=nil || booking.Status!="booked" { t.Fatalf("create provider booking: %#v %v",booking,err) }
	var eventCount int
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM outbox_events WHERE aggregate_type='provider_booking' AND aggregate_id=$1`,booking.ID).Scan(&eventCount); err!=nil || eventCount!=1 {
		t.Fatalf("booking reservation outbox event: count=%d err=%v",eventCount,err)
	}
	second:=createOrder("booking-second-"+fmt.Sprint(time.Now().UnixNano()),start.Add(45*time.Minute))
	if _,err=store.CreateProviderBooking(ctx,operator,provider.ID,second.ID,start.Add(30*time.Minute),start.Add(time.Hour),loc.String()); !errors.Is(err,ErrProviderBookingConflict) {
		t.Fatalf("overlapping provider booking must fail transactionally: %v",err)
	}
	cancellation,err:=store.CancelOrder(ctx,customer,first.ID,"cancel-booking-"+fmt.Sprint(time.Now().UnixNano()),"Customer cancelled reserved order")
	if err!=nil || cancellation.FeeMinor!=0 { t.Fatalf("cancel order should release provider slot: %#v %v",cancellation,err) }
	var bookingStatus string
	if err=pool.QueryRow(ctx,`SELECT status FROM provider_bookings WHERE id=$1`,booking.ID).Scan(&bookingStatus); err!=nil || bookingStatus!="cancelled" {
		t.Fatalf("order cancellation must release provider booking: status=%s err=%v",bookingStatus,err)
	}
	secondBooking,err:=store.CreateProviderBooking(ctx,operator,provider.ID,second.ID,start.Add(30*time.Minute),start.Add(time.Hour),loc.String())
	if err!=nil || secondBooking.Status!="booked" { t.Fatalf("released slot should be reservable: %#v %v",secondBooking,err) }
	outside:=createOrder("booking-outside-"+fmt.Sprint(time.Now().UnixNano()),end.Add(30*time.Minute))
	if _,err=store.CreateProviderBooking(ctx,operator,provider.ID,outside.ID,end.Add(15*time.Minute),end.Add(time.Hour),loc.String()); !errors.Is(err,ErrProviderUnavailable) {
		t.Fatalf("booking outside available window must fail: %v",err)
	}
	if _,err=store.SetProviderAvailability(ctx,professional,provider.ID,"unavailable",start.Add(15*time.Minute),start.Add(45*time.Minute),loc.String()); !errors.Is(err,ErrAvailabilityOverlap) {
		t.Fatalf("unavailable window cannot overlap a confirmed booking: %v",err)
	}
	if _,err=pool.Exec(ctx,`UPDATE provider_bookings SET end_at=end_at+interval '1 hour' WHERE id=$1`,booking.ID); err==nil {
		t.Fatal("provider booking reservation must be immutable")
	}
	if _,err=pool.Exec(ctx,`UPDATE provider_availability_windows SET timezone='UTC' WHERE id=$1`,window.ID); err==nil {
		t.Fatal("provider availability windows must be immutable")
	}
}
