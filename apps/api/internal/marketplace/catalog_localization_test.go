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
	for _,name:=range []string{"004_catalog.sql","007_market_config.sql","026_market_config_audit.sql","008_catalog_localization.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	operator:=Actor{SubjectID:"catalog-operator",TenantID:"platform",Role:RoleOperator}
	if err=ensureTestOperatorGrants(ctx,pool,operator,ActionMarketConfigure,ActionCatalogManage); err!=nil { t.Fatal(err) }

	protectedRoot,err:=store.CreateCategoryForActor(ctx,operator,nil,fmt.Sprintf("protected-%d",time.Now().UnixNano()),"Protected root")
	if err!=nil { t.Fatalf("authorized category creation: %v",err) }
	source,err:=store.CreateCategoryForActor(ctx,operator,nil,fmt.Sprintf("source-%d",time.Now().UnixNano()),"Source")
	if err!=nil { t.Fatal(err) }
	if err=store.MoveCategoryForActor(ctx,operator,source.ID,&protectedRoot.ID); err!=nil { t.Fatalf("authorized category move: %v",err) }
	moved,err:=store.GetCategory(ctx,source.ID)
	if err!=nil || moved.ParentID==nil || *moved.ParentID!=protectedRoot.ID { t.Fatalf("authorized move parent mismatch: %#v %v",moved,err) }
	if err=store.MoveCategoryForActor(ctx,operator,protectedRoot.ID,&source.ID); !errors.Is(err,ErrInvalidCategory) {
		t.Fatalf("authorized category move must reject cycles: %v",err)
	}
	if err=store.SetCategoryEnabledForActor(ctx,operator,protectedRoot.ID,false); err!=nil { t.Fatal(err) }
	if _,err=store.CreateCategoryForActor(ctx,operator,&protectedRoot.ID,"blocked-child","Blocked child"); !errors.Is(err,ErrCategoryDisabled) {
		t.Fatalf("authorized category creation beneath disabled ancestor must fail: %v",err)
	}
	if err=store.SetCategoryEnabledForActor(ctx,operator,protectedRoot.ID,true); err!=nil { t.Fatal(err) }
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
	child,err:=store.CreateCategory(ctx,&root.ID,fmt.Sprintf("plumbing-%d",time.Now().UnixNano()),"Plumbing")
	if err!=nil { t.Fatal(err) }
	if err=store.SetCategoryVisibility(ctx,operator,"OM",child.ID,true); err!=nil { t.Fatal(err) }
	if err=store.SetCategoryTranslation(ctx,operator,"OM",child.ID,"en","Plumbing","Localized plumbing description"); err!=nil { t.Fatal(err) }
	if _,err=store.ResolveLocalizedCategory(ctx,"OM","en",child.ID); err!=nil { t.Fatalf("visible child with visible ancestor should resolve: %v",err) }
	listed,err:=store.ListLocalizedCategories(ctx,"OM","en",nil)
	if err!=nil || len(listed)!=2 { t.Fatalf("visible category listing should include root and child: %#v %v",listed,err) }
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
	if _,err=store.ResolveLocalizedCategory(ctx,"OM","en",child.ID); !errors.Is(err,ErrCategoryUnavailable) {
		t.Fatalf("visible child beneath a market-hidden ancestor must not resolve: %v",err)
	}
	hiddenAncestorList,err:=store.ListLocalizedCategories(ctx,"OM","en",nil)
	if err!=nil || len(hiddenAncestorList)!=0 {
		t.Fatalf("market-hidden ancestor must hide its entire subtree: %#v %v",hiddenAncestorList,err)
	}
	if err=store.SetCategoryVisibility(ctx,operator,"OM",root.ID,true); err!=nil { t.Fatal(err) }
	if err=store.SetCategoryEnabled(ctx,root.ID,false); err!=nil { t.Fatal(err) }
	disabledList,err:=store.ListLocalizedCategories(ctx,"OM","en",nil)
	if err!=nil || len(disabledList)!=0 {
		t.Fatalf("disabled category must not be listed: %#v %v",disabledList,err)
	}
	if err=store.SetCategoryVisibility(ctx,operator,"ZZ",root.ID,true); !errors.Is(err,ErrMarketUnavailable) {
		t.Fatalf("unknown market must fail closed: %v",err)
	}
	support:=Actor{SubjectID:"support",TenantID:"platform",Role:RoleSupport}
	if _,err=store.CreateCategoryForActor(ctx,support,nil,"support-created","Support created"); !errors.Is(err,ErrForbidden) {
		t.Fatalf("support must not create categories: %v",err)
	}
	if err=store.MoveCategoryForActor(ctx,support,root.ID,nil); !errors.Is(err,ErrForbidden) {
		t.Fatalf("support must not move categories: %v",err)
	}
	if err=store.SetCategoryEnabledForActor(ctx,support,root.ID,false); !errors.Is(err,ErrForbidden) {
		t.Fatalf("support must not change category lifecycle: %v",err)
	}
	if err=store.SetCategoryTranslation(ctx,support,"OM",root.ID,"en","Blocked",""); !errors.Is(err,ErrForbidden) {
		t.Fatalf("support must not edit catalog translations: %v",err)
	}
	if err=store.SetCategoryVisibility(ctx,support,"OM",root.ID,false); !errors.Is(err,ErrForbidden) {
		t.Fatalf("support must not change catalog visibility: %v",err)
	}
}
