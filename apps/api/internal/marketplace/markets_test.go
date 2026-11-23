package marketplace

import (
	"context"
	"encoding/json"
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
	for _,name:=range []string{"007_market_config.sql","026_market_config_audit.sql"} {
		migration,err:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if err!=nil { t.Fatal(err) }
		if _,err=execMigrationScript(ctx,pool,migration); err!=nil { t.Fatalf("market migration %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	operator:=Actor{SubjectID:"operator-market-test-"+time.Now().Format("20060102150405.000000000"),TenantID:"platform",Role:RoleOperator}
	config,err:=store.SaveMarket(ctx,operator,MarketConfig{
		Code:"om",DefaultLocale:"en",Currency:"omr",Timezone:"Asia/Muscat",
		PolicyVersion:"2026-01",Enabled:true,Locales:[]string{"en","ar","en"},
	})
	if err!=nil { t.Fatal(err) }
	if config.Code!="OM" || config.Currency!="OMR" || config.Version<1 || len(config.Locales)!=2 {
		t.Fatalf("unexpected saved market: %#v",config)
	}
	resolved,locale,err:=store.ResolveMarketLocale(ctx,"OM","ar")
	if err!=nil || locale!="ar" || resolved.Timezone!="Asia/Muscat" { t.Fatalf("resolve Arabic locale: %#v %s %v",resolved,locale,err) }
	_,locale,err=store.ResolveMarketLocale(ctx,"OM","fr")
	if err!=nil || locale!="en" { t.Fatalf("unsupported locale should fall back to default: %s %v",locale,err) }
	config.Enabled=false
	updated,err:=store.SaveMarket(ctx,operator,config)
	if err!=nil || updated.Version!=config.Version+1 { t.Fatalf("market update should increment version: %#v %v",updated,err) }
	if _,err=store.GetMarket(ctx,"OM"); !errors.Is(err,ErrMarketUnavailable) { t.Fatalf("disabled market must fail closed: %v",err) }
	if _,err=store.GetMarket(ctx,"ZZ"); !errors.Is(err,ErrMarketUnavailable) { t.Fatalf("unknown market must fail closed: %v",err) }
	if _,err=store.SaveMarket(ctx,Actor{SubjectID:"support",TenantID:"platform",Role:RoleSupport},config); !errors.Is(err,ErrForbidden) {
		t.Fatalf("support must not configure markets: %v",err)
	}
	if _,err=store.SaveMarket(ctx,Actor{SubjectID:"tenant-operator",TenantID:"tenant-a",Role:RoleOperator},config); !errors.Is(err,ErrForbidden) {
		t.Fatalf("tenant operator must not mutate global market configuration: %v",err)
	}

	rows,err:=pool.Query(ctx,`
		SELECT action,COALESCE(previous_version,0),new_version,before_config,after_config
		FROM market_config_audit WHERE actor_subject_id=$1 ORDER BY id
	`,operator.SubjectID)
	if err!=nil { t.Fatal(err) }
	type auditRow struct { action string; previousVersion,newVersion int; before,after []byte }
	var audit []auditRow
	for rows.Next() {
		var row auditRow
		if err=rows.Scan(&row.action,&row.previousVersion,&row.newVersion,&row.before,&row.after); err!=nil {
			rows.Close()
			t.Fatal(err)
		}
		audit=append(audit,row)
	}
	if err=rows.Err(); err!=nil { rows.Close(); t.Fatal(err) }
	rows.Close()
	if len(audit)!=2 || audit[0].newVersion!=config.Version || audit[1].action!="updated" ||
		audit[1].previousVersion!=config.Version || audit[1].newVersion!=updated.Version ||
		len(audit[1].before)==0 || len(audit[1].after)==0 {
		t.Fatalf("unexpected append-only market config audit: %#v",audit)
	}
	var beforeConfig,afterConfig map[string]any
	if err=json.Unmarshal(audit[1].before,&beforeConfig); err!=nil { t.Fatal(err) }
	if err=json.Unmarshal(audit[1].after,&afterConfig); err!=nil { t.Fatal(err) }
	if beforeConfig["enabled"]!=true || afterConfig["enabled"]!=false {
		t.Fatalf("audit snapshots do not preserve before/after config: before=%#v after=%#v",beforeConfig,afterConfig)
	}
	if _,err=pool.Exec(ctx,`UPDATE market_config_audit SET action='tampered' WHERE actor_subject_id=$1`,operator.SubjectID); err==nil {
		t.Fatal("market config audit must reject UPDATE")
	}
	if _,err=pool.Exec(ctx,`DELETE FROM market_config_audit WHERE actor_subject_id=$1`,operator.SubjectID); err==nil {
		t.Fatal("market config audit must reject DELETE")
	}
	tx,err:=pool.Begin(ctx)
	if err!=nil { t.Fatal(err) }
	if _,err=tx.Exec(ctx,`TRUNCATE market_config_audit`); err==nil {
		_ = tx.Rollback(ctx)
		t.Fatal("market config audit must reject TRUNCATE")
	}
	_ = tx.Rollback(ctx)
	config.Timezone="Not/A_Timezone"
	if _,err=store.SaveMarket(ctx,operator,config); !errors.Is(err,ErrInvalidMarket) { t.Fatalf("invalid timezone must fail: %v",err) }
}
