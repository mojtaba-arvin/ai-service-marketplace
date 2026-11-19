package marketplace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMarketConfigurationAndLocaleResolutionPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	migration,err:=os.ReadFile(filepath.Join("..","..","migrations","007_market_config.sql"))
	if err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,string(migration)); err!=nil { t.Fatalf("market migration: %v",err) }
	store:=&Store{DB:pool}
	operator:=Actor{SubjectID:"operator-market-test",TenantID:"platform",Role:RoleOperator}
	config,err:=store.SaveMarket(ctx,operator,MarketConfig{
		Code:"om",DefaultLocale:"en",Currency:"omr",Timezone:"Asia/Muscat",
		PolicyVersion:"2026-01",Enabled:true,Locales:[]string{"en","ar","en"},
	})
	if err!=nil { t.Fatal(err) }
	if config.Code!="OM" || config.Currency!="OMR" || config.Version!=1 || len(config.Locales)!=2 {
		t.Fatalf("unexpected saved market: %#v",config)
	}
	resolved,locale,err:=store.ResolveMarketLocale(ctx,"OM","ar")
	if err!=nil || locale!="ar" || resolved.Timezone!="Asia/Muscat" { t.Fatalf("resolve Arabic locale: %#v %s %v",resolved,locale,err) }
	_,locale,err=store.ResolveMarketLocale(ctx,"OM","fr")
	if err!=nil || locale!="en" { t.Fatalf("unsupported locale should fall back to default: %s %v",locale,err) }
	config.Enabled=false
	updated,err:=store.SaveMarket(ctx,operator,config)
	if err!=nil || updated.Version!=2 { t.Fatalf("market update should increment version: %#v %v",updated,err) }
	if _,err=store.GetMarket(ctx,"OM"); !errors.Is(err,ErrMarketUnavailable) { t.Fatalf("disabled market must fail closed: %v",err) }
	if _,err=store.GetMarket(ctx,"ZZ"); !errors.Is(err,ErrMarketUnavailable) { t.Fatalf("unknown market must fail closed: %v",err) }
	if _,err=store.SaveMarket(ctx,Actor{SubjectID:"support",TenantID:"platform",Role:RoleSupport},config); !errors.Is(err,ErrForbidden) {
		t.Fatalf("support must not configure markets: %v",err)
	}
	config.Timezone="Not/A_Timezone"
	if _,err=store.SaveMarket(ctx,operator,config); !errors.Is(err,ErrInvalidMarket) { t.Fatalf("invalid timezone must fail: %v",err) }
}
