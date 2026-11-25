package main

import (
 "context"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "testing"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/analytics"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func TestAnalyticsRebuildCatchesLateCommitAndCutsOverAtomicallyPostgres(t *testing.T) {
 dsn:=os.Getenv("TEST_DATABASE_URL")
 if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second);defer cancel()
 suffix:=fmt.Sprint(time.Now().UnixNano())
 schema:="analytics_rebuild_"+suffix
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 if _,err=admin.Exec(ctx,"CREATE SCHEMA "+schema);err!=nil{t.Fatal(err)}
 defer func(){cleanupCtx,cleanupCancel:=context.WithTimeout(context.Background(),5*time.Second);defer cleanupCancel();_,_ = admin.Exec(cleanupCtx,"DROP SCHEMA "+schema+" CASCADE")}()
 config,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)}
 config.ConnConfig.RuntimeParams["search_path"]=schema
 pool,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil{t.Fatal(err)};defer pool.Close()
 for _,name:=range []string{"001_walking_skeleton.sql","019_outbox_delivery.sql","030_analytics_event_projection.sql"}{
  raw,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name));if readErr!=nil{t.Fatal(readErr)}
  if _,err=pool.Exec(ctx,string(raw),pgx.QueryExecModeSimpleProtocol);err!=nil{t.Fatalf("apply %s: %v",name,err)}
 }
 tenant:="analytics-rebuild-tenant-"+suffix
 oldGeneration:="old-"+suffix
 generation:="rebuilt-"+suffix
 consumerName:="analytics:"+tenant+":"+generation
 oldConsumer:="analytics:"+tenant+":"+oldGeneration
 if err=pool.QueryRow(ctx,`INSERT INTO analytics_projection_generations(tenant_id,generation_id,consumer_name,status,activated_at)
 VALUES($1,$2,$3,'active',now()) RETURNING generation_id`,tenant,oldGeneration,oldConsumer).Scan(new(string));err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO analytics_active_generations(tenant_id,generation_id) VALUES($1,$2)`,tenant,oldGeneration);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO analytics_projection_generations(tenant_id,generation_id,consumer_name,status)
 VALUES($1,$2,$3,'building')`,tenant,generation,consumerName);err!=nil{t.Fatal(err)}

 appendEvent:=func(orderID,eventID string) int64 {
  t.Helper()
  data,_:=json.Marshal(map[string]string{"order_id":orderID,"status":"requested","price_version_id":"price-v1","scheduled_at":"2026-11-26T10:00:00Z"})
  tx,beginErr:=pool.BeginTx(ctx,pgx.TxOptions{});if beginErr!=nil{t.Fatal(beginErr)}
  id,appendErr:=outbox.AppendDomainEvent(ctx,tx,outbox.DomainEvent{ID:eventID,Type:"order.created",Version:1,OccurredAt:time.Now().UTC(),TenantID:tenant,Aggregate:outbox.EventAggregate{Type:"order",ID:orderID},Metadata:map[string]string{"market_code":"OM","traffic_class":"production"},Data:data})
  if appendErr!=nil{_ = tx.Rollback(ctx);t.Fatal(appendErr)}
  if commitErr:=tx.Commit(ctx);commitErr!=nil{t.Fatal(commitErr)}
  return id
 }
 appendEvent("order-a-"+suffix,"event-a-"+suffix)
 consumer:=outbox.Consumer{DB:pool,Name:consumerName,Handler:(analytics.Projector{DB:pool,ConsumerName:consumerName,GenerationID:generation,TenantID:tenant}).Handle}
 drain:=func() {
  t.Helper()
  for {
   batch,loadErr:=loadPending(ctx,pool,tenant,consumerName,100);if loadErr!=nil{t.Fatal(loadErr)}
   if len(batch)==0{return}
   for _,event:=range batch{if _,processErr:=consumer.Process(ctx,event);processErr!=nil{t.Fatalf("process %d: %v",event.ID,processErr)}}
  }
 }
 drain()
 // A producer may commit after an apparently empty scan. No ID cursor is
 // retained, so the next pass discovers this late commit even if its sequence
 // value sorts before events already seen by a previous pass.
 appendEvent("order-b-"+suffix,"event-b-"+suffix)
 if cutoverErr:=cutover(ctx,pool,tenant,generation,consumerName);cutoverErr==nil{
  t.Fatal("cutover must reject a generation with a committed late event")
 }
 drain()
 validated,err:=validateGeneration(ctx,pool,tenant,generation,consumerName)
 if err!=nil||validated.Source!=2||validated.Facts!=2||validated.Pending!=0{t.Fatalf("validation: %+v err=%v",validated,err)}
 if err=cutover(ctx,pool,tenant,generation,consumerName);err!=nil{t.Fatal(err)}
 var active string
 if err=pool.QueryRow(ctx,`SELECT generation_id FROM analytics_active_generations WHERE tenant_id=$1`,tenant).Scan(&active);err!=nil||active!=generation{t.Fatalf("active generation=%q err=%v",active,err)}
 var oldStatus,newStatus string
 if err=pool.QueryRow(ctx,`SELECT status FROM analytics_projection_generations WHERE tenant_id=$1 AND generation_id=$2`,tenant,oldGeneration).Scan(&oldStatus);err!=nil{t.Fatal(err)}
 if err=pool.QueryRow(ctx,`SELECT status FROM analytics_projection_generations WHERE tenant_id=$1 AND generation_id=$2`,tenant,generation).Scan(&newStatus);err!=nil{t.Fatal(err)}
 if oldStatus!="retired"||newStatus!="active"{t.Fatalf("generation states old=%q new=%q",oldStatus,newStatus)}
 if err=runRollback(ctx,pool,tenant,oldGeneration);err!=nil{t.Fatalf("rollback must catch up and atomically restore prior generation: %v",err)}
 if err=pool.QueryRow(ctx,`SELECT generation_id FROM analytics_active_generations WHERE tenant_id=$1`,tenant).Scan(&active);err!=nil||active!=oldGeneration{t.Fatalf("rollback active generation=%q err=%v",active,err)}
}
