package marketplace

import(
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)
func TestServiceFormVersionDraftPublishAndImmutabilityPostgres(t *testing.T){
 dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
 for _,name:=range []string{"001_walking_skeleton.sql","004_catalog.sql","005_pricing.sql","007_market_config.sql","026_market_config_audit.sql","027_service_form_versions.sql"}{
  raw,e:=os.ReadFile(filepath.Join("..","..","migrations",name));if e!=nil{t.Fatal(e)}
  if _,e=execMigrationScript(ctx,pool,raw);e!=nil{t.Fatalf("apply %s: %v",name,e)}
 }
 suffix,err:=newID();if err!=nil{t.Fatal(err)}
 marketCode:="T"+strings.ToUpper(strings.NewReplacer("0","A","1","B","2","C","3","D","4","E","5","F","6","G","7","H","8","I","9","J").Replace(suffix[:7]))
 if _,err=pool.Exec(ctx,`INSERT INTO markets(code,default_locale,currency,timezone,policy_version,enabled,version) VALUES($1,'en','OMR','Asia/Muscat','v1',true,1) ON CONFLICT(code) DO NOTHING`,marketCode);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO market_locales(market_code,locale,is_default) VALUES($1,'en',true) ON CONFLICT DO NOTHING`,marketCode);err!=nil{t.Fatal(err)}
 categoryID,menuID:="form-category-"+suffix,"form-menu-"+suffix
 if _,err=pool.Exec(ctx,`INSERT INTO service_categories(id,slug,display_name) VALUES($1,$2,'Air conditioning')`,categoryID,"form-"+suffix[:8]);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO service_price_menus(id,category_id,service_code) VALUES($1,$2,'ac-repair')`,menuID,categoryID);err!=nil{t.Fatal(err)}
 store:=&Store{DB:pool}
 platform:=Actor{SubjectID:"form-admin-"+suffix,TenantID:platformTenantID,Role:RoleOperator}
 invalidSchema:=[]byte(`{"type":"object","properties":{},"patternProperties":{}}`)
 if _,err=store.CreateServiceFormDraft(ctx,platform,menuID,marketCode,"en",invalidSchema);!errors.Is(err,ErrInvalidServiceForm){t.Fatalf("unsupported schema keyword accepted: %v",err)}
 schema1:=[]byte(`{"type":"object","required":["rooms"],"properties":{"rooms":{"type":"integer","minimum":1,"maximum":20},"unit_model":{"type":"string","maxLength":80}}}`)
 draft,err:=store.CreateServiceFormDraft(ctx,platform,menuID,marketCode,"en",schema1);if err!=nil{t.Fatal(err)}
 if draft.Version!=1||draft.Status!="draft"||draft.PublishedAt!=nil{t.Fatalf("unexpected draft: %#v",draft)}
 if _,err=store.GetPublishedServiceForm(ctx,menuID,marketCode,"en");!errors.Is(err,ErrServiceFormNotFound){t.Fatalf("draft became visible: %v",err)}
 if _,err=store.CreateServiceFormDraft(ctx,Actor{SubjectID:"tenant-operator",TenantID:"tenant-a",Role:RoleOperator},menuID,marketCode,"en",schema1);!errors.Is(err,ErrForbidden){t.Fatalf("non-platform operator created global form: %v",err)}
 if _,err=store.CreateServiceFormDraft(ctx,platform,menuID,"ZZ","en",schema1);!errors.Is(err,ErrMarketUnavailable){t.Fatalf("unknown market accepted: %v",err)}
 published,err:=store.PublishServiceFormVersion(ctx,platform,draft.ID);if err!=nil{t.Fatal(err)}
 if published.Status!="published"||published.PublishedAt==nil||published.PublishedBy==nil||*published.PublishedBy!=platform.SubjectID{t.Fatalf("unexpected published form: %#v",published)}
 if _,err=store.PublishServiceFormVersion(ctx,platform,draft.ID);err!=nil{t.Fatalf("publishing same version should be idempotent: %v",err)}
 active,err:=store.GetPublishedServiceForm(ctx,menuID,marketCode,"en");if err!=nil{t.Fatal(err)}
 if active.ID!=draft.ID||active.Version!=1{t.Fatalf("wrong active version: %#v",active)}
 schema2:=[]byte(`{"type":"object","required":["rooms","urgent"],"properties":{"rooms":{"type":"integer","minimum":1,"maximum":30},"urgent":{"type":"boolean"}}}`)
 second,err:=store.CreateServiceFormDraft(ctx,platform,menuID,marketCode,"en",schema2);if err!=nil{t.Fatal(err)}
 if second.Version!=2{t.Fatalf("expected version 2, got %d",second.Version)}
 secondPublished,err:=store.PublishServiceFormVersion(ctx,platform,second.ID);if err!=nil{t.Fatal(err)}
 if secondPublished.Status!="published"{t.Fatalf("second version not published: %#v",secondPublished)}
 var oldStatus string
 if err=pool.QueryRow(ctx,`SELECT status FROM service_form_versions WHERE id=$1`,draft.ID).Scan(&oldStatus);err!=nil{t.Fatal(err)}
 if oldStatus!="retired"{t.Fatalf("previous published version was not retired: %s",oldStatus)}
 active,err=store.GetPublishedServiceForm(ctx,menuID,marketCode,"en");if err!=nil{t.Fatal(err)};if active.ID!=second.ID{t.Fatalf("new version is not active: %#v",active)}
 if _,err=pool.Exec(ctx,`UPDATE service_form_versions SET schema='{"type":"object","properties":{"bad":{"type":"string"}}}'::jsonb WHERE id=$1`,draft.ID);err==nil{t.Fatal("retired form schema mutation was allowed")}
 if _,err=pool.Exec(ctx,`DELETE FROM service_form_versions WHERE id=$1`,draft.ID);err==nil{t.Fatal("service form version deletion was allowed")}
 var eventCount int
 if err=pool.QueryRow(ctx,`SELECT count(*) FROM outbox_events WHERE aggregate_type='service_form' AND aggregate_id=$1 AND event_type='catalog.service_form.published'`,second.ID).Scan(&eventCount);err!=nil{t.Fatal(err)}
 if eventCount!=1{t.Fatalf("expected one publish event, got %d",eventCount)}
 var eventType,tenantID,dataFormID string
 if err=pool.QueryRow(ctx,`SELECT payload->>'type',payload->>'tenant_id',payload->'data'->>'form_version_id' FROM outbox_events WHERE aggregate_type='service_form' AND aggregate_id=$1 AND event_type='catalog.service_form.published'`,second.ID).Scan(&eventType,&tenantID,&dataFormID);err!=nil{t.Fatal(err)}
 if eventType!="catalog.service_form.published"||tenantID!=platformTenantID||dataFormID!=second.ID{t.Fatalf("invalid canonical publication event: %s %s %s",eventType,tenantID,dataFormID)}
 if err=pool.QueryRow(ctx,`SELECT status FROM service_form_versions WHERE id=$1`,second.ID).Scan(&oldStatus);err!=nil{t.Fatal(err)}
 if oldStatus!="published"{t.Fatal("new version is not active")}
 if _,err=pool.Exec(ctx,`UPDATE service_form_versions SET status='draft' WHERE id=$1`,second.ID);err==nil{t.Fatal("published form was reactivated")}
 if _,err=pool.Exec(ctx,`UPDATE service_form_versions SET published_by='attacker' WHERE id=$1`,second.ID);err==nil{t.Fatal("published metadata mutation was allowed")}
 if _,err=pool.Exec(ctx,`UPDATE service_form_versions SET retired_by='attacker' WHERE id=$1`,draft.ID);err==nil{t.Fatal("retirement metadata mutation was allowed")}
 if _,err=pool.Exec(ctx,`TRUNCATE TABLE service_form_versions`);err==nil{t.Fatal("service form history truncate was allowed")}
 var wg sync.WaitGroup
 versions:=make(chan int,2)
 failures:=make(chan error,2)
 for i:=0;i<2;i++{wg.Add(1);go func(){defer wg.Done();v,e:=store.CreateServiceFormDraft(ctx,platform,menuID,marketCode,"en",schema1);if e!=nil{failures<-e;return};versions<-v.Version}()}
 wg.Wait();close(versions);close(failures)
 for e:=range failures{t.Fatal(e)}
 got:=map[int]bool{};for v:=range versions{got[v]=true}
 if !got[3]||!got[4]{t.Fatalf("concurrent draft versions were not serialized: %#v",got)}
}
