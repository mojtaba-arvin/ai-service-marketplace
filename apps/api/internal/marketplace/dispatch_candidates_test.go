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
	if err=ensureTestOperatorGrants(ctx,pool,marketOperator,ActionMarketConfigure); err!=nil { t.Fatal(err) }
	if err=ensureTestOperatorGrants(ctx,pool,operator,ActionProviderVerify,ActionOrderAssign); err!=nil { t.Fatal(err) }
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

	// An invalid candidate window must roll back the protected read transaction.
	if _,err=store.FindDispatchCandidatesForOrder(ctx,operator,order.ID,start,start); !errors.Is(err,ErrInvalidAvailability) {
		t.Fatalf("invalid candidate window should fail closed: %v",err)
	}
	var orderStatus string
	if err=pool.QueryRow(ctx,`SELECT status FROM orders WHERE id=$1 AND tenant_id=$2`,order.ID,operator.TenantID).Scan(&orderStatus); err!=nil || orderStatus!="requested" {
		t.Fatalf("failed read must not mutate order state: status=%q err=%v",orderStatus,err)
	}

	// Queue a state transition ahead of the protected read while both contend
	// on the same order row. The dispatch transaction must observe the committed
	// non-requested state rather than a stale pre-transition snapshot.
	stateBlocker,err:=pool.Begin(ctx)
	if err!=nil { t.Fatal(err) }
	var lockedOrderID string
	if err=stateBlocker.QueryRow(ctx,`SELECT id FROM orders WHERE id=$1 FOR UPDATE`,order.ID).Scan(&lockedOrderID); err!=nil {
		_ = stateBlocker.Rollback(ctx); t.Fatal(err)
	}
	stateChangeDone:=make(chan error,1)
	go func() {
		_,changeErr:=pool.Exec(ctx,`UPDATE orders SET status='accepted' WHERE id=$1`,order.ID)
		stateChangeDone<-changeErr
	}()
	waitForDBLock:=func(pattern string) {
		t.Helper()
		deadline:=time.Now().Add(5*time.Second)
		for time.Now().Before(deadline) {
			var waiting bool
			checkErr:=pool.QueryRow(ctx,`
				SELECT EXISTS(SELECT 1 FROM pg_stat_activity
				WHERE datname=current_database() AND pid<>pg_backend_pid()
				AND wait_event_type='Lock' AND query ILIKE $1)
			`,pattern).Scan(&waiting)
			if checkErr!=nil { t.Fatal(checkErr) }
			if waiting { return }
			time.Sleep(10*time.Millisecond)
		}
		t.Fatalf("timed out waiting for PostgreSQL lock: %s",pattern)
	}
	waitForDBLock("%UPDATE orders SET status%")
	stateDispatchDone:=make(chan error,1)
	go func() {
		_,callErr:=store.FindDispatchCandidatesForOrder(ctx,operator,order.ID,start,end)
		stateDispatchDone<-callErr
	}()
	waitForDBLock("%FROM orders%")
	if err=stateBlocker.Commit(ctx); err!=nil { t.Fatal(err) }
	if err=<-stateChangeDone; err!=nil { t.Fatal(err) }
	if err=<-stateDispatchDone; !errors.Is(err,ErrProviderUnavailable) {
		t.Fatalf("dispatch must observe concurrent accepted state: %v",err)
	}
	if _,err=pool.Exec(ctx,`UPDATE orders SET status='requested' WHERE id=$1`,order.ID); err!=nil { t.Fatal(err) }

	// Even an otherwise authorized operator cannot resolve an order ID outside
	// the tenant bound to the authenticated Actor.
	if _,err=pool.Exec(ctx,`UPDATE orders SET tenant_id='market-foreign' WHERE id=$1`,order.ID); err!=nil { t.Fatal(err) }
	if _,err=store.FindDispatchCandidatesForOrder(ctx,operator,order.ID,start,end); !errors.Is(err,ErrOrderNotFound) {
		t.Fatalf("authorized operator must not distinguish a foreign-tenant order: %v",err)
	}
	if _,err=pool.Exec(ctx,`UPDATE orders SET tenant_id=$2 WHERE id=$1`,order.ID,operator.TenantID); err!=nil { t.Fatal(err) }

	// Prepare a separate permission administrator to revoke the dispatch grant.
	adminID:=newTestIdentity(t,ctx,pool)
	adminTenant:=operator.TenantID
	if _,err=pool.Exec(ctx,`INSERT INTO identity_memberships(id,identity_id,tenant_id,role) VALUES($1,$2,$3,'operator')`,
		"dispatch-admin-membership-"+adminID,adminID,adminTenant); err!=nil { t.Fatal(err) }
	adminGrantID:="dispatch-admin-grant-"+adminID
	if _,err=pool.Exec(ctx,`
		INSERT INTO operator_action_grants(id,identity_id,tenant_id,action,granted_by_kind,grant_reason)
		VALUES($1,$2,$3,'operator:permissions:manage','system_bootstrap','Dispatch race test administrator')
	`,adminGrantID,adminID,adminTenant); err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`
		INSERT INTO operator_permission_events(grant_id,tenant_id,identity_id,action,event_type,actor_kind,reason,after_state)
		VALUES($1,$2,$3,'operator:permissions:manage','granted','system_bootstrap',
			'Dispatch race test administrator','{"active":true}'::jsonb)
	`,adminGrantID,adminTenant,adminID); err!=nil { t.Fatal(err) }
	var dispatchGrantID string
	if err=pool.QueryRow(ctx,`
		SELECT id FROM operator_action_grants
		WHERE identity_id=$1 AND tenant_id=$2 AND action='order:assign' AND revoked_at IS NULL
	`,operator.SubjectID,operator.TenantID).Scan(&dispatchGrantID); err!=nil { t.Fatal(err) }

	// Hold the order row so the dispatch call pauses after acquiring the
	// authorization FOR SHARE locks. Revocation must then wait for the complete
	// protected read, not merely for an earlier standalone authorization check.
	blockerTx,err:=pool.Begin(ctx)
	if err!=nil { t.Fatal(err) }
	if err=blockerTx.QueryRow(ctx,`SELECT id FROM orders WHERE id=$1 FOR UPDATE`,order.ID).Scan(&lockedOrderID); err!=nil {
		_ = blockerTx.Rollback(ctx); t.Fatal(err)
	}
	type dispatchResult struct { candidates []DispatchCandidate; err error }
	dispatchDone:=make(chan dispatchResult,1)
	go func() {
		items,callErr:=store.FindDispatchCandidatesForOrder(ctx,operator,order.ID,start,end)
		dispatchDone<-dispatchResult{candidates:items,err:callErr}
	}()
	waitForLock:=func(pattern string) {
		t.Helper()
		deadline:=time.Now().Add(5*time.Second)
		for time.Now().Before(deadline) {
			var waiting bool
			checkErr:=pool.QueryRow(ctx,`
				SELECT EXISTS(SELECT 1 FROM pg_stat_activity
				WHERE datname=current_database() AND pid<>pg_backend_pid()
				AND wait_event_type='Lock' AND query ILIKE $1)
			`,pattern).Scan(&waiting)
			if checkErr!=nil { t.Fatal(checkErr) }
			if waiting { return }
			time.Sleep(10*time.Millisecond)
		}
		t.Fatalf("timed out waiting for PostgreSQL lock: %s",pattern)
	}
	waitForLock("%FROM orders%")
	type revokeResult struct { grant OperatorActionGrant; err error }
	revokeDone:=make(chan revokeResult,1)
	admin:=Actor{SubjectID:adminID,TenantID:adminTenant,Role:RoleOperator}
	go func() {
		grant,revokeErr:=store.RevokeOperatorAction(ctx,admin,adminTenant,dispatchGrantID,"Dispatch race regression")
		revokeDone<-revokeResult{grant:grant,err:revokeErr}
	}()
	waitForLock("%operator_action_grants%")
	if err=blockerTx.Commit(ctx); err!=nil { t.Fatal(err) }
	select {
	case result:=<-dispatchDone:
		if result.err!=nil { t.Fatalf("in-flight protected read should complete before revocation: %v",result.err) }
	case <-ctx.Done(): t.Fatal("dispatch read did not finish after releasing order lock")
	}
	select {
	case result:=<-revokeDone:
		if result.err!=nil || result.grant.RevokedAt==nil { t.Fatalf("serialized revocation failed: %+v %v",result.grant,result.err) }
	case <-ctx.Done(): t.Fatal("revocation did not finish after protected read committed")
	}
	if _,err=store.FindDispatchCandidatesForOrder(ctx,operator,order.ID,start,end); !errors.Is(err,ErrForbidden) {
		t.Fatalf("revoked order:assign grant must deny replay: %v",err)
	}

	// A stale tenant context and a role without persisted operator membership
	// cannot use the same order ID.
	staleActor:=Actor{SubjectID:operator.SubjectID,TenantID:"market-stale-context",Role:RoleOperator}
	if _,err=store.FindDispatchCandidatesForOrder(ctx,staleActor,order.ID,start,end); !errors.Is(err,ErrForbidden) {
		t.Fatalf("stale tenant context must deny: %v",err)
	}
	missingMembership:=Actor{SubjectID:proA.SubjectID,TenantID:operator.TenantID,Role:RoleOperator}
	if _,err=store.FindDispatchCandidatesForOrder(ctx,missingMembership,order.ID,start,end); !errors.Is(err,ErrForbidden) {
		t.Fatalf("operator role without membership/grant must deny: %v",err)
	}

	// Once the order belongs to another tenant, its ID is indistinguishable
	// from an unknown order to the original tenant.
	if _,err=pool.Exec(ctx,`UPDATE orders SET tenant_id='market-foreign' WHERE id=$1`,order.ID); err!=nil { t.Fatal(err) }
	if _,err=store.FindDispatchCandidatesForOrder(ctx,customer,order.ID,start,end); !errors.Is(err,ErrOrderNotFound) {
		t.Fatalf("cross-tenant order ID must be indistinguishable from missing: %v",err)
	}
}
