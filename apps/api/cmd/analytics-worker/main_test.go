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
 processedEvent:=batch[0]
 if applied,processErr:=consumer.Process(ctx,processedEvent);processErr!=nil||!applied { t.Fatalf("process event: applied=%v err=%v",applied,processErr) }
 batch,err=loadBatch(ctx,pool,tenant,consumerName,100)
 if err!=nil||len(batch)!=0 { t.Fatalf("processed event must not be delivered again: remaining=%d err=%v",len(batch),err) }
 var facts int
 if err=pool.QueryRow(ctx,`SELECT count(*) FROM analytics_event_facts WHERE tenant_id=$1 AND generation_id=$2 AND event_id=$3`,tenant,generation,domain.ID).Scan(&facts);err!=nil { t.Fatal(err) }
 if facts!=1 { t.Fatalf("expected exactly one generation fact, got %d",facts) }
 summary,err:=analytics.QuerySummary(ctx,pool,tenant,analytics.SummaryFilter{From:time.Now().UTC().Add(-time.Minute),To:time.Now().UTC().Add(time.Minute),MarketCode:"OM"})
 if err!=nil||summary.OrdersCreated!=1||!summary.DataReady { t.Fatalf("summary must read the active generation only: %+v err=%v",summary,err) }
 // A shadow generation can be populated independently without leaking into summaries.
 nextGeneration:="shadow-"+suffix
 nextConsumerName:="analytics:"+tenant+":"+nextGeneration
 if _,err=pool.Exec(ctx,"INSERT INTO analytics_projection_generations(tenant_id,generation_id,consumer_name,status) VALUES($1,$2,$3,'building')",tenant,nextGeneration,nextConsumerName);err!=nil { t.Fatal(err) }
 nextConsumer:=outbox.Consumer{DB:pool,Name:nextConsumerName,Handler:(analytics.Projector{DB:pool,ConsumerName:nextConsumerName,GenerationID:nextGeneration,TenantID:tenant}).Handle}
 if applied,processErr:=nextConsumer.Process(ctx,processedEvent);processErr!=nil||!applied { t.Fatalf("replay into shadow generation: applied=%v err=%v",applied,processErr) }
 secondOrder:="analytics-worker-order-shadow-"+suffix
 secondID:="analytics-worker-event-shadow-"+suffix
 secondData,marshalErr:=json.Marshal(map[string]string{"order_id":secondOrder,"status":"requested","price_version_id":"price-v2","scheduled_at":"2026-11-27T10:00:00Z"})
 if marshalErr!=nil { t.Fatal(marshalErr) }
 secondDomain:=outbox.DomainEvent{ID:secondID,Type:"order.created",Version:1,OccurredAt:time.Now().UTC(),TenantID:tenant,Aggregate:outbox.EventAggregate{Type:"order",ID:secondOrder},Metadata:map[string]string{"market_code":"OM","traffic_class":"production"},Data:secondData}
 secondTx,beginErr:=pool.BeginTx(ctx,pgx.TxOptions{});if beginErr!=nil { t.Fatal(beginErr) }
 secondOutboxID,appendErr:=outbox.AppendDomainEvent(ctx,secondTx,secondDomain)
 if appendErr!=nil { _=secondTx.Rollback(ctx);t.Fatal(appendErr) }
 if err=secondTx.Commit(ctx);err!=nil { t.Fatal(err) }
 var secondEvent outbox.Event
 if err=pool.QueryRow(ctx,"SELECT id,aggregate_type,aggregate_id,event_type,payload,attempts FROM outbox_events WHERE id=$1",secondOutboxID).Scan(&secondEvent.ID,&secondEvent.AggregateType,&secondEvent.AggregateID,&secondEvent.EventType,&secondEvent.Payload,&secondEvent.Attempts);err!=nil { t.Fatal(err) }
 if applied,processErr:=nextConsumer.Process(ctx,secondEvent);processErr!=nil||!applied { t.Fatalf("process shadow event: applied=%v err=%v",applied,processErr) }
 beforeSwitch,err:=analytics.QuerySummary(ctx,pool,tenant,analytics.SummaryFilter{From:time.Now().UTC().Add(-time.Minute),To:time.Now().UTC().Add(time.Minute),MarketCode:"OM"})
 if err!=nil||beforeSwitch.OrdersCreated!=1 { t.Fatalf("shadow facts must remain invisible before cutover: %+v err=%v",beforeSwitch,err) }
 switchTx,err:=pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil { t.Fatal(err) }
 if _,err=switchTx.Exec(ctx,"UPDATE analytics_active_generations SET generation_id=$2,activated_at=now() WHERE tenant_id=$1",tenant,nextGeneration);err!=nil { _=switchTx.Rollback(ctx);t.Fatal(err) }
 if _,err=switchTx.Exec(ctx,"UPDATE analytics_projection_generations SET status='retired',retired_at=now() WHERE tenant_id=$1 AND generation_id=$2",tenant,generation);err!=nil { _=switchTx.Rollback(ctx);t.Fatal(err) }
 if _,err=switchTx.Exec(ctx,"UPDATE analytics_projection_generations SET status='active',activated_at=now() WHERE tenant_id=$1 AND generation_id=$2",tenant,nextGeneration);err!=nil { _=switchTx.Rollback(ctx);t.Fatal(err) }
 if err=switchTx.Commit(ctx);err!=nil { t.Fatal(err) }
 afterSwitch,err:=analytics.QuerySummary(ctx,pool,tenant,analytics.SummaryFilter{From:time.Now().UTC().Add(-time.Minute),To:time.Now().UTC().Add(time.Minute),MarketCode:"OM"})
 if err!=nil||afterSwitch.OrdersCreated!=2 { t.Fatalf("summary must switch atomically to the new generation: %+v err=%v",afterSwitch,err) }
}
