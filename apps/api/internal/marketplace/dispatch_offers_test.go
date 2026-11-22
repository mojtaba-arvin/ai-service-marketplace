package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func TestDispatchOffersSingleWinnerDeclineAndExpiryPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","004_catalog.sql","005_pricing.sql","006_provider_eligibility.sql","007_market_config.sql","008_catalog_localization.sql","015_order_schedule.sql","022_identity_memberships.sql","023_order_tenant_scope.sql","016_cancellation_policy.sql","017_provider_availability.sql","018_dispatch_offers.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	ids:=make([]string,4)
	for i:=range ids {
		ids[i],err=newID()
		if err!=nil { t.Fatal(err) }
		phone:=fmt.Sprintf("+1980%011d",(time.Now().UnixNano()+int64(i))%100_000_000_000)
		if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,ids[i],phone); err!=nil { t.Fatal(err) }
	}
	customer:=Actor{SubjectID:ids[0],TenantID:"market-dispatch",Role:RoleCustomer}
	proA:=Actor{SubjectID:ids[1],TenantID:"market-dispatch",Role:RoleProfessional}
	proB:=Actor{SubjectID:ids[2],TenantID:"market-dispatch",Role:RoleProfessional}
	operator:=Actor{SubjectID:ids[3],TenantID:"market-dispatch",Role:RoleOperator}
	if _,err=store.SaveMarket(ctx,operator,MarketConfig{Code:"OM",DefaultLocale:"en",Currency:"OMR",Timezone:"Asia/Muscat",PolicyVersion:"dispatch-offers-v1",Enabled:true,Locales:[]string{"en","ar"}}); err!=nil { t.Fatal(err) }
	category,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("offers-%d",time.Now().UnixNano()),"Offers")
	if err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO market_category_visibility(market_code,category_id,visible) VALUES('OM',$1,true)`,category.ID);err!=nil { t.Fatal(err) }
	menu,err:=store.CreatePriceMenu(ctx,category.ID,"dispatch-service")
	if err!=nil { t.Fatal(err) }
	if _,err=store.CreatePriceVersion(ctx,menu.ID,3000,"OMR"); err!=nil { t.Fatal(err) }
	createProvider:=func(actor Actor,name string) ProviderProfile {
		p,createErr:=store.CreateProviderProfile(ctx,actor,name)
		if createErr!=nil { t.Fatal(createErr) }
		if createErr=store.AddProviderSkill(ctx,actor,p.ID,category.ID); createErr!=nil { t.Fatal(createErr) }
		if createErr=store.AddProviderServiceArea(ctx,actor,p.ID,"OM","Muscat","Muscat","Al-Khuwair"); createErr!=nil { t.Fatal(createErr) }
		if createErr=store.VerifyProviderProfile(ctx,operator,p.ID); createErr!=nil { t.Fatal(createErr) }
		if createErr=store.VerifyProviderSkill(ctx,operator,p.ID,category.ID); createErr!=nil { t.Fatal(createErr) }
		if createErr=store.SetProviderActive(ctx,operator,p.ID,true); createErr!=nil { t.Fatal(createErr) }
		return p
	}
	providerA:=createProvider(proA,"Provider A")
	providerB:=createProvider(proB,"Provider B")
	loc,err:=time.LoadLocation("Asia/Muscat")
	if err!=nil { t.Fatal(err) }
	base:=time.Now().In(loc).Add(3*time.Hour).Truncate(time.Minute)
	if _,err=store.SetProviderAvailability(ctx,proA,providerA.ID,"available",base.Add(-time.Hour),base.Add(4*time.Hour),loc.String()); err!=nil { t.Fatal(err) }
	if _,err=store.SetProviderAvailability(ctx,proB,providerB.ID,"available",base.Add(-time.Hour),base.Add(4*time.Hour),loc.String()); err!=nil { t.Fatal(err) }

	order,created,err:=store.CreateScheduledOrder(ctx,customer,"dispatch-offer-race-"+fmt.Sprint(time.Now().UnixNano()),menu.ID,base,"OM","Muscat","Muscat","Al-Khuwair")
	if err!=nil || !created { t.Fatalf("create scheduled order: %#v %v",order,err) }
	expires:=time.Now().UTC().Add(5*time.Minute)
	wrongTenantOperator:=operator
	wrongTenantOperator.TenantID="market-other"
	if _,err=store.CreateDispatchOffers(ctx,wrongTenantOperator,order.ID,[]string{providerA.ID},1,expires); !errors.Is(err,ErrForbidden) {
		t.Fatalf("operator from another tenant must not create offers: %v",err)
	}
	offers,err:=store.CreateDispatchOffers(ctx,operator,order.ID,[]string{providerA.ID,providerB.ID},2,expires)
	if err!=nil || len(offers)!=2 { t.Fatalf("create two bounded offers: %#v %v",offers,err) }
	wrongTenantProfessional:=proA
	wrongTenantProfessional.TenantID="market-other"
	if _,err=store.AcceptDispatchOffer(ctx,wrongTenantProfessional,offers[0].ID); !errors.Is(err,ErrForbidden) {
		t.Fatalf("professional from another tenant must not accept an offer: %v",err)
	}
	if _,err=pool.Exec(ctx,`UPDATE dispatch_rounds SET round_no=round_no+1 WHERE id=$1`,offers[0].RoundID); err==nil {
		t.Fatal("dispatch rounds must be immutable audit records")
	}
	if _,err=pool.Exec(ctx,`UPDATE dispatch_offers SET provider_id=$2 WHERE id=$1`,offers[0].ID,providerB.ID); err==nil {
		t.Fatal("dispatch offer recipient must be immutable")
	}
	if _,err=store.CreateDispatchOffers(ctx,operator,order.ID,[]string{providerA.ID},1,expires); !errors.Is(err,ErrDispatchRoundActive) {
		t.Fatalf("active offer round must prevent duplicate fan-out: %v",err)
	}
	var wg sync.WaitGroup
	type result struct { assignment DispatchAssignment; err error }
	results:=make(chan result,2)
	for _,offer:=range offers {
		wg.Add(1)
		offerID,offerProviderID:=offer.ID,offer.ProviderID
		go func(id,providerID string) {
			defer wg.Done()
			assignment,acceptErr:=store.AcceptDispatchOffer(context.Background(),map[string]Actor{providerA.ID:proA,providerB.ID:proB}[providerID],id)
			results<-result{assignment:assignment,err:acceptErr}
		}(offerID,offerProviderID)
	}
	wg.Wait()
	close(results)
	wins:=0
	for result:=range results {
		if result.err==nil { wins++; continue }
		if !errors.Is(result.err,ErrOrderAlreadyAssigned) && !errors.Is(result.err,ErrDispatchOfferUnavailable) {
			t.Fatalf("unexpected competing acceptance result: %v",result.err)
		}
	}
	if wins!=1 { t.Fatalf("exactly one provider must win, got %d",wins) }
	var assignments,bookings int
	var assignedProvider string
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM dispatch_assignments WHERE order_id=$1`,order.ID).Scan(&assignments); err!=nil { t.Fatal(err) }
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM provider_bookings WHERE order_id=$1 AND status='booked'`,order.ID).Scan(&bookings); err!=nil { t.Fatal(err) }
	if err=pool.QueryRow(ctx,`SELECT assigned_provider_id FROM orders WHERE id=$1 AND status='accepted'`,order.ID).Scan(&assignedProvider); err!=nil { t.Fatal(err) }
	if assignments!=1 || bookings!=1 || assignedProvider=="" { t.Fatalf("single-winner invariants failed: assignments=%d bookings=%d provider=%q",assignments,bookings,assignedProvider) }
	var superseded int
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM dispatch_offers WHERE order_id=$1 AND status='superseded'`,order.ID).Scan(&superseded); err!=nil { t.Fatal(err) }
	if superseded!=1 { t.Fatalf("losing offer should be superseded, got %d",superseded) }

	declineOrder,created,err:=store.CreateScheduledOrder(ctx,customer,"dispatch-offer-decline-"+fmt.Sprint(time.Now().UnixNano()),menu.ID,base.Add(90*time.Minute),"OM","Muscat","Muscat","Al-Khuwair")
	if err!=nil || !created { t.Fatalf("create decline order: %#v %v",declineOrder,err) }
	declineOffers,err:=store.CreateDispatchOffers(ctx,operator,declineOrder.ID,[]string{providerA.ID},1,time.Now().UTC().Add(5*time.Minute))
	if err!=nil { t.Fatal(err) }
	if err=store.DeclineDispatchOffer(ctx,proA,declineOffers[0].ID); err!=nil { t.Fatalf("decline own offer: %v",err) }
	var declined string
	if err=pool.QueryRow(ctx,`SELECT status FROM dispatch_offers WHERE id=$1`,declineOffers[0].ID).Scan(&declined); err!=nil || declined!="declined" {
		t.Fatalf("declined offer state mismatch: %q %v",declined,err)
	}
	if err=store.DeclineDispatchOffer(ctx,proB,declineOffers[0].ID); !errors.Is(err,ErrForbidden) {
		t.Fatalf("another provider cannot decline offer: %v",err)
	}

	expiredRound,err:=newID()
	if err!=nil { t.Fatal(err) }
	expiredOffer,err:=newID()
	if err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO dispatch_rounds(id,order_id,round_no,fanout_limit,offer_count,expires_at,created_by_identity_id) VALUES($1,$2,2,1,1,now()-interval '1 minute',$3)`,expiredRound,declineOrder.ID,ids[3]); err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO dispatch_offers(id,round_id,order_id,provider_id,start_at,end_at,timezone,expires_at) VALUES($1,$2,$3,$4,$5,$6,'Asia/Muscat',now()-interval '1 minute')`,expiredOffer,expiredRound,declineOrder.ID,providerB.ID,base,base.Add(30*time.Minute)); err!=nil { t.Fatal(err) }
	expiredCount,err:=store.ExpireDispatchOffers(ctx,25)
	if err!=nil || expiredCount!=1 { t.Fatalf("expire due offers: count=%d err=%v",expiredCount,err) }
	var expiredStatus string
	if err=pool.QueryRow(ctx,`SELECT status FROM dispatch_offers WHERE id=$1`,expiredOffer).Scan(&expiredStatus); err!=nil || expiredStatus!="expired" {
		t.Fatalf("expired offer state mismatch: %q %v",expiredStatus,err)
	}
	eventScopes:=map[string][]string{
		"dispatch.offer_created":{offers[0].ID,offers[1].ID},
		"order.provider_assigned":{order.ID},
		"dispatch.offer_declined":{declineOffers[0].ID},
		"dispatch.offer_expired":{expiredOffer},
	}
	for eventType,aggregateIDs:=range eventScopes {
		var got int
		if err=pool.QueryRow(ctx,`SELECT count(*) FROM outbox_events WHERE event_type=$1 AND aggregate_id=ANY($2::text[])`,eventType,aggregateIDs).Scan(&got); err!=nil || got!=len(aggregateIDs) {
			t.Fatalf("outbox event %s count=%d want=%d err=%v",eventType,got,len(aggregateIDs),err)
		}
		for _,aggregateID:=range aggregateIDs {
			var raw []byte
			if err=pool.QueryRow(ctx,`SELECT payload FROM outbox_events WHERE event_type=$1 AND aggregate_id=$2`,eventType,aggregateID).Scan(&raw); err!=nil { t.Fatal(err) }
			var event outbox.DomainEvent
			if err=json.Unmarshal(raw,&event); err!=nil { t.Fatalf("decode %s event: %v",eventType,err) }
			if err=outbox.ValidateDomainEvent(event); err!=nil || event.TenantID!=operator.TenantID {
				t.Fatalf("%s must be canonical and tenant-scoped: event=%+v err=%v",eventType,event,err)
			}
		}
	}
}
