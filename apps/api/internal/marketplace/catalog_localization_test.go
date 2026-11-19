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

func TestMarketLocalizedCatalogVisibilityPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"004_catalog.sql","007_market_config.sql","008_catalog_localization.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=pool.Exec(ctx,string(migration)); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	operator:=Actor{SubjectID:"catalog-operator",TenantID:"platform",Role:RoleOperator}
	for _,config:=range []MarketConfig{
		{Code:"OM",DefaultLocale:"en",Currency:"OMR",Timezone:"Asia/Muscat",PolicyVersion:"v1",Enabled:true,Locales:[]string{"en","ar"}},
		{Code:"DE",DefaultLocale:"de",Currency:"EUR",Timezone:"Europe/Berlin",PolicyVersion:"v1",Enabled:true,Locales:[]string{"de"}},
	} {
		if _,err=store.SaveMarket(ctx,operator,config); err!=nil { t.Fatal(err) }
	}
	root,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("home-%d",time.Now().UnixNano()),"Home")
	if err!=nil { t.Fatal(err) }
	if err=store.SetCategoryVisibility(ctx,operator,"OM",root.ID,true); err!=nil { t.Fatal(err) }
	if err=store.SetCategoryVisibility(ctx,operator,"DE",root.ID,true); err!=nil { t.Fatal(err) }
	for _,translation:=range []struct{market,locale,name string}{
		{"OM","en","Home services"},{"OM","ar","خدمات المنزل"},{"DE","de","Haushaltsdienste"},
	} {
		if err=store.SetCategoryTranslation(ctx,operator,translation.market,root.ID,translation.locale,translation.name,"Localized category description"); err!=nil { t.Fatal(err) }
	}
	om,err:=store.ResolveLocalizedCategory(ctx,"OM","ar",root.ID)
	if err!=nil || om.DisplayName!="خدمات المنزل" || om.Locale!="ar" { t.Fatalf("Arabic category: %#v %v",om,err) }
	de,err:=store.ResolveLocalizedCategory(ctx,"DE","de",root.ID)
	if err!=nil || de.DisplayName!="Haushaltsdienste" || de.MarketCode!="DE" { t.Fatalf("German category: %#v %v",de,err) }
	fallback,err:=store.ResolveLocalizedCategory(ctx,"OM","fr",root.ID)
	if err!=nil || fallback.Locale!="en" || fallback.DisplayName!="Home services" { t.Fatalf("default locale fallback: %#v %v",fallback,err) }
	listed,err:=store.ListLocalizedCategories(ctx,"OM","en",nil)
	if err!=nil || len(listed)!=1 || listed[0].ID!=root.ID { t.Fatalf("visible category listing: %#v %v",listed,err) }
	if _,err=store.ResolveLocalizedCategory(ctx,"DE","en",root.ID); err!=nil { t.Fatalf("unsupported locale should resolve to market default: %v",err) }

	hidden,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("hidden-%d",time.Now().UnixNano()),"Hidden")
	if err!=nil { t.Fatal(err) }
	if _,err=store.ResolveLocalizedCategory(ctx,"OM","en",hidden.ID); !errors.Is(err,ErrCategoryUnavailable) {
		t.Fatalf("market/category without explicit visibility must not list: %v",err)
	}
	if err=store.SetCategoryVisibility(ctx,operator,"OM",root.ID,false); err!=nil { t.Fatal(err) }
	if _,err=store.ResolveLocalizedCategory(ctx,"OM","en",root.ID); !errors.Is(err,ErrCategoryUnavailable) {
		t.Fatalf("hidden category must not resolve: %v",err)
	}
	if err=store.SetCategoryVisibility(ctx,operator,"OM",root.ID,true); err!=nil { t.Fatal(err) }
	if err=store.SetCategoryEnabled(ctx,root.ID,false); err!=nil { t.Fatal(err) }
	if _,err=store.ListLocalizedCategories(ctx,"OM","en",nil); err!=nil || len(listed)>1 {
		t.Fatalf("disabled category must not be listed: %#v %v",listed,err)
	}
	if err=store.SetCategoryVisibility(ctx,operator,"ZZ",root.ID,true); !errors.Is(err,ErrMarketUnavailable) {
		t.Fatalf("unknown market must fail closed: %v",err)
	}
	if err=store.SetCategoryTranslation(ctx,Actor{SubjectID:"support",TenantID:"platform",Role:RoleSupport},"OM",root.ID,"en","Blocked",""); !errors.Is(err,ErrForbidden) {
		t.Fatalf("support must not edit catalog translations: %v",err)
	}
}
