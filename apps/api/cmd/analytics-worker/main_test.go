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

func TestAnalyticsWorkerConsumesTenantEventsIdempotentlyPostgres(t *testing.T) {
 dsn:=os.Getenv("TEST_DATABASE_URL")
 if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
 ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second)
 defer cancel()
 pool,err:=pgxpool.New(ctx,dsn)
 if err!=nil { t.Fatal(err) }
 defer pool.Close()
 for _,name:=range []string{"001_walking_skeleton.sql","019_outbox_delivery.sql","030_analytics_event_projection.sql"} {
  raw,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
  if readErr!=nil { t.Fatal(readErr) }
  if _,err=pool.Exec(ctx,string(raw),pgx.QueryExecModeSimpleProtocol);err!=nil { t.Fatalf("apply %s: %v",name,err) }
 }
 suffix:=fmt.Sprint(time.Now().UnixNano())
 tenant:="analytics-worker-tenant-"+suffix
 generation:="initial-"+suffix
 consumerName:="analytics:"+tenant+":"+generation
 if err=ensureActiveGeneration(ctx,pool,tenant,generation,consumerName);err!=nil { t.Fatal(err) }
 if err=ensureActiveGeneration(ctx,pool,tenant,generation,consumerName);err!=nil { t.Fatalf("restart must preserve active generation: %v",err) }
 if err=ensureActiveGeneration(ctx,pool,tenant,"unexpected-"+suffix,"analytics:"+tenant+":unexpected-"+suffix);err==nil {
  t.Fatal("worker must refuse a non-active generation")
 }
 domain:=outbox.DomainEvent{
  ID:"analytics-worker-event-"+suffix,Type:"order.created",Version:1,
  OccurredAt:time.Now().UTC(),TenantID:tenant,
  Aggregate:outbox.EventAggregate{Type:"order",ID:"analytics-worker-order-"+suffix},
  Metadata:map[string]string{"market_code":"OM","traffic_class":"production"},
  Data:json.RawMessage(`{"order_id":"analytics-worker-order-`+suffix+`","status":"requested","price_version_id":"price-v1","scheduled_at":"2026-11-26T10:00:00Z"}`),
 }
 tx,err:=pool.BeginTx(ctx,pgx.TxOptions{})
 if err!=nil { t.Fatal(err) }
 eventID,err:=outbox.AppendDomainEvent(ctx,tx,domain)
 if err!=nil { _=tx.Rollback(ctx);t.Fatal(err) }
 if err=tx.Commit(ctx);err!=nil { t.Fatal(err) }

 batch,err:=loadBatch(ctx,pool,tenant,consumerName,100)
 if err!=nil||len(batch)!=1||batch[0].ID!=eventID { t.Fatalf("expected one pending tenant event, got %d err=%v",len(batch),err) }
 consumer:=outbox.Consumer{DB:pool,Name:consumerName,Handler:(analytics.Projector{DB:pool,ConsumerName:consumerName,GenerationID:generation,TenantID:tenant}).Handle}
 if applied,processErr:=consumer.Process(ctx,batch[0]);processErr!=nil||!applied { t.Fatalf("process event: applied=%v err=%v",applied,processErr) }
 batch,err=loadBatch(ctx,pool,tenant,consumerName,100)
 if err!=nil||len(batch)!=0 { t.Fatalf("processed event must not be delivered again: remaining=%d err=%v",len(batch),err) }
 var facts int
 if err=pool.QueryRow(ctx,`SELECT count(*) FROM analytics_event_facts WHERE tenant_id=$1 AND generation_id=$2 AND event_id=$3`,tenant,generation,domain.ID).Scan(&facts);err!=nil { t.Fatal(err) }
 if facts!=1 { t.Fatalf("expected exactly one generation fact, got %d",facts) }
 summary,err:=analytics.QuerySummary(ctx,pool,tenant,analytics.SummaryFilter{From:time.Now().UTC().Add(-time.Minute),To:time.Now().UTC().Add(time.Minute),MarketCode:"OM"})
 if err!=nil||summary.OrdersCreated!=1||!summary.DataReady { t.Fatalf("summary must read the active generation only: %+v err=%v",summary,err) }
}
