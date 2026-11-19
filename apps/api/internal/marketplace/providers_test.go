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

func TestProviderEligibilityPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"002_identity.sql","004_catalog.sql","006_provider_eligibility.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=pool.Exec(ctx,string(migration)); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	category,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("electrical-%d",time.Now().UnixNano()),"Electrical")
	if err!=nil { t.Fatal(err) }
	identityIDs:=make([]string,3)
	for i:=range identityIDs {
		identityIDs[i],err=newID()
		if err!=nil { t.Fatal(err) }
		phone:=fmt.Sprintf("+1996%011d",(time.Now().UnixNano()+int64(i))%100_000_000_000)
		if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,identityIDs[i],phone); err!=nil { t.Fatal(err) }
	}
	professional1:=Actor{SubjectID:identityIDs[0],TenantID:"market-a",Role:RoleProfessional}
	professional2:=Actor{SubjectID:identityIDs[1],TenantID:"market-a",Role:RoleProfessional}
	operator:=Actor{SubjectID:identityIDs[2],TenantID:"market-a",Role:RoleOperator}
	provider1,err:=store.CreateProviderProfile(ctx,professional1,"Verified electrician")
	if err!=nil { t.Fatal(err) }
	provider2,err:=store.CreateProviderProfile(ctx,professional2,"Unverified electrician")
	if err!=nil { t.Fatal(err) }
	for _,provider:=range []struct{actor Actor;id string}{{professional1,provider1.ID},{professional2,provider2.ID}} {
		if err=store.AddProviderSkill(ctx,provider.actor,provider.id,category.ID); err!=nil { t.Fatal(err) }
		if err=store.AddProviderServiceArea(ctx,provider.actor,provider.id,"om","Muscat","Muscat","Al-Khuwair"); err!=nil { t.Fatal(err) }
	}
	if err=store.VerifyProviderProfile(ctx,Actor{SubjectID:identityIDs[2],TenantID:"market-b",Role:RoleOperator},provider1.ID); !errors.Is(err,ErrForbidden) {
		t.Fatalf("cross-tenant operator must not verify profile: %v",err)
	}
	if err=store.VerifyProviderProfile(ctx,operator,provider1.ID); err!=nil { t.Fatal(err) }
	if err=store.VerifyProviderSkill(ctx,operator,provider1.ID,category.ID); err!=nil { t.Fatal(err) }
	if err=store.SetProviderActive(ctx,operator,provider1.ID,true); err!=nil { t.Fatal(err) }
	if err=store.SetProviderActive(ctx,operator,provider2.ID,true); !errors.Is(err,ErrProviderNotEligible) {
		t.Fatalf("unverified provider must not activate: %v",err)
	}
	candidates,err:=store.FindEligibleProviders(ctx,category.ID,"OM","Muscat","Muscat","Al-Khuwair")
	if err!=nil { t.Fatal(err) }
	if len(candidates)!=1 || candidates[0].ProviderID!=provider1.ID {
		t.Fatalf("expected only verified eligible provider, got %#v",candidates)
	}
	wrongArea,err:=store.FindEligibleProviders(ctx,category.ID,"OM","Dhofar","Salalah","Central")
	if err!=nil || len(wrongArea)!=0 { t.Fatalf("wrong area must return no providers: %#v %v",wrongArea,err) }
	if err=store.SetCategoryEnabled(ctx,category.ID,false); err!=nil { t.Fatal(err) }
	candidates,err=store.FindEligibleProviders(ctx,category.ID,"OM","Muscat","Muscat","Al-Khuwair")
	if err!=nil || len(candidates)!=0 { t.Fatalf("disabled category must return no candidates: %#v %v",candidates,err) }
}
