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

func TestDispatchCandidatesRequireAllEligibilityAndAvailabilityGatesPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","004_catalog.sql","005_pricing.sql","006_provider_eligibility.sql","007_market_config.sql","026_market_config_audit.sql","008_catalog_localization.sql","015_order_schedule.sql","022_identity_memberships.sql","023_order_tenant_scope.sql","026_market_config_audit.sql","027_service_form_versions.sql","028_order_service_form_answers.sql","016_cancellation_policy.sql","017_provider_availability.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	ids:=make([]string,4)
	for i:=range ids {
		ids[i],err=newID()
		if err!=nil { t.Fatal(err) }
		phone:=fmt.Sprintf("+1990%011d",(time.Now().UnixNano()+int64(i))%100_000_000_000)
		if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,ids[i],phone); err!=nil { t.Fatal(err) }
	}
	customer:=Actor{SubjectID:ids[0],TenantID:"market-dispatch",Role:RoleCustomer}
	proA:=Actor{SubjectID:ids[1],TenantID:"market-dispatch",Role:RoleProfessional}
	proB:=Actor{SubjectID:ids[2],TenantID:"market-dispatch",Role:RoleProfessional}
	operator:=Actor{SubjectID:ids[3],TenantID:"market-dispatch",Role:RoleOperator}
	marketOperator:=Actor{SubjectID:ids[3],TenantID:platformTenantID,Role:RoleOperator}
	if _,err=store.SaveMarket(ctx,marketOperator,MarketConfig{Code:"OM",DefaultLocale:"en",Currency:"OMR",Timezone:"Asia/Muscat",PolicyVersion:"dispatch-candidates-v1",Enabled:true,Locales:[]string{"en","ar"}}); err!=nil { t.Fatal(err) }
	category,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("dispatch-%d",time.Now().UnixNano()),"Dispatch")
	if err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO market_category_visibility(market_code,category_id,visible) VALUES('OM',$1,true)`,category.ID);err!=nil { t.Fatal(err) }
	menu,err:=store.CreatePriceMenu(ctx,category.ID,"dispatch-service")
	if err!=nil { t.Fatal(err) }
	if _,err=store.CreatePriceVersion(ctx,menu.ID,3000,"OMR"); err!=nil { t.Fatal(err) }

	createProvider:=func(actor Actor,name string) ProviderProfile {
		provider,createErr:=store.CreateProviderProfile(ctx,actor,name)
		if createErr!=nil { t.Fatal(createErr) }
		if createErr=store.AddProviderSkill(ctx,actor,provider.ID,category.ID); createErr!=nil { t.Fatal(createErr) }
		if createErr=store.AddProviderServiceArea(ctx,actor,provider.ID,"OM","Muscat","Muscat","Al-Khuwair"); createErr!=nil { t.Fatal(createErr) }
		if createErr=store.VerifyProviderProfile(ctx,operator,provider.ID); createErr!=nil { t.Fatal(createErr) }
		if createErr=store.VerifyProviderSkill(ctx,operator,provider.ID,category.ID); createErr!=nil { t.Fatal(createErr) }
		if createErr=store.SetProviderActive(ctx,operator,provider.ID,true); createErr!=nil { t.Fatal(createErr) }
		return provider
	}
	providerA:=createProvider(proA,"Available provider")
	_ = createProvider(proB,"Provider without calendar")
	loc,err:=time.LoadLocation("Asia/Muscat")
	if err!=nil { t.Fatal(err) }
	start:=time.Now().In(loc).Add(2*time.Hour).Truncate(time.Minute)
	end:=start.Add(90*time.Minute)
	if _,err=store.SetProviderAvailability(ctx,proA,providerA.ID,"available",start.Add(-15*time.Minute),end.Add(15*time.Minute),loc.String()); err!=nil {
		t.Fatal(err)
	}
	order,created,err:=store.CreateScheduledOrder(ctx,customer,"dispatch-order-"+fmt.Sprint(time.Now().UnixNano()),menu.ID,
		start.Add(30*time.Minute),"OM","Muscat","Muscat","Al-Khuwair")
	if err!=nil || !created { t.Fatalf("create dispatch order: %#v %v",order,err) }

	candidates,err:=store.FindDispatchCandidatesForOrder(ctx,customer,order.ID,start,end)
	if err!=nil { t.Fatal(err) }
	if len(candidates)!=1 || candidates[0].ProviderID!=providerA.ID {
		t.Fatalf("only fully eligible and available provider should appear: %#v",candidates)
	}
	if _,err=store.FindDispatchCandidatesForOrder(ctx,Actor{SubjectID:ids[2],TenantID:"market-dispatch",Role:RoleCustomer},order.ID,start,end); !errors.Is(err,ErrForbidden) {
		t.Fatalf("another customer cannot search dispatch candidates for this order: %v",err)
	}
	wrongArea,queryErr:=store.FindDispatchCandidates(ctx,customer,category.ID,"OM","Muscat","Seeb","Al-Khuwair",start,end)
	if queryErr!=nil || len(wrongArea)!=0 { t.Fatalf("wrong area must exclude providers: %#v %v",wrongArea,queryErr) }
	if _,err=store.CreateProviderBooking(ctx,operator,providerA.ID,order.ID,start,end,loc.String()); err!=nil {
		t.Fatalf("reserve eligible provider: %v",err)
	}
	afterBooking,err:=store.FindDispatchCandidatesForOrder(ctx,customer,order.ID,start,end)
	if err!=nil || len(afterBooking)!=0 { t.Fatalf("booked provider must not be offered again: %#v %v",afterBooking,err) }
	if err=store.SetCategoryEnabled(ctx,category.ID,false); err!=nil { t.Fatal(err) }
	disabled,err:=store.FindDispatchCandidatesForOrder(ctx,customer,order.ID,start,end)
	if err!=nil || len(disabled)!=0 { t.Fatalf("disabled category must exclude every candidate: %#v %v",disabled,err) }
}
