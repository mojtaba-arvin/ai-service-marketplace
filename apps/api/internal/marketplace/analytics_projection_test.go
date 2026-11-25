package marketplace

import (
 "context"
 "encoding/json"
 "fmt"
 "strings"
 "os"
 "path/filepath"
 "testing"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/analytics"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func TestAnalyticsProjectionMinimizesAndQuarantinesEventsPostgres(t *testing.T) {
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
  if _,err=execMigrationScript(ctx,pool,raw);err!=nil { t.Fatalf("apply %s: %v",name,err) }
 }
 suffix:=fmt.Sprint(time.Now().UnixNano())
 tenant:="analytics-privacy-tenant-"+suffix
 generation:="privacy-v1-"+suffix
 consumerName:="analytics:"+tenant+":"+generation
 if _,err=pool.Exec(ctx,"INSERT INTO analytics_projection_generations(tenant_id,generation_id,consumer_name,status,activated_at) VALUES($1,$2,$3,'active',now())",tenant,generation,consumerName);err!=nil { t.Fatal(err) }
 if _,err=pool.Exec(ctx,"INSERT INTO analytics_active_generations(tenant_id,generation_id) VALUES($1,$2)",tenant,generation);err!=nil { t.Fatal(err) }

 orderID:="analytics-privacy-order-"+suffix
 canonical:=outbox.DomainEvent{ID:"analytics-privacy-event-"+suffix,Type:"order.created",Version:1,OccurredAt:time.Now().UTC(),TenantID:tenant,
  Aggregate:outbox.EventAggregate{Type:"order",ID:orderID},Metadata:map[string]string{"market_code":"OM","traffic_class":"production"},
  Data:json.RawMessage(fmt.Sprintf("{"order_id":%q,"status":"requested","price_version_id":"price-v1","scheduled_at":"2026-11-26T10:00:00Z"}",orderID))}
 unsafe:=canonical
 unsafe.ID="analytics-unsafe-event-"+suffix
 unsafe.Aggregate=outbox.EventAggregate{Type:"order",ID:"analytics-unsafe-order-"+suffix}
 unsafe.Data=json.RawMessage(fmt.Sprintf("{"order_id":%q,"status":"requested","price_version_id":"price-v1","scheduled_at":"2026-11-26T10:00:00Z","phoneNumber":"+96890000000"}",unsafe.Aggregate.ID))
 appendDomain:=func(event outbox.DomainEvent) int64 {
  tx,beginErr:=pool.BeginTx(ctx,pgx.TxOptions{});if beginErr!=nil { t.Fatal(beginErr) }
  id,appendErr:=outbox.AppendDomainEvent(ctx,tx,event)
  if appendErr!=nil { _=tx.Rollback(ctx);t.Fatal(appendErr) }
  if commitErr:=tx.Commit(ctx);commitErr!=nil { t.Fatal(commitErr) }
  return id
 }
 canonicalOutboxID:=appendDomain(canonical)
 unsafeOutboxID:=appendDomain(unsafe)
 legacyTx,err:=pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil { t.Fatal(err) }
 legacyID,err:=outbox.Append(ctx,legacyTx,"order","legacy-"+suffix,"order.created",json.RawMessage("{"phone":"+96890000000"}"))
 if err!=nil { _=legacyTx.Rollback(ctx);t.Fatal(err) }
 if err=legacyTx.Commit(ctx);err!=nil { t.Fatal(err) }

 projector:=analytics.Projector{DB:pool,ConsumerName:consumerName,GenerationID:generation,TenantID:tenant}
 consumer:=projector.Consumer()
 for _,id:=range []int64{canonicalOutboxID,unsafeOutboxID,legacyID} {
  var event outbox.Event
  if err=pool.QueryRow(ctx,"SELECT id,aggregate_type,aggregate_id,event_type,payload,attempts FROM outbox_events WHERE id=$1",id).Scan(&event.ID,&event.AggregateType,&event.AggregateID,&event.EventType,&event.Payload,&event.Attempts);err!=nil { t.Fatal(err) }
  if applied,processErr:=consumer.Process(ctx,event);processErr!=nil||!applied { t.Fatalf("process %d: applied=%v err=%v",id,applied,processErr) }
 }
 var facts,quarantined int
 if err=pool.QueryRow(ctx,"SELECT count(*) FROM analytics_event_facts WHERE tenant_id=$1 AND generation_id=$2",tenant,generation).Scan(&facts);err!=nil { t.Fatal(err) }
 if err=pool.QueryRow(ctx,"SELECT count(*) FROM analytics_event_quarantine WHERE tenant_id=$1 AND generation_id=$2",tenant,generation).Scan(&quarantined);err!=nil { t.Fatal(err) }
 if facts!=1||quarantined!=2 { t.Fatalf("expected one minimized fact and two quarantined events: facts=%d quarantined=%d",facts,quarantined) }
 var projected string
 if err=pool.QueryRow(ctx,"SELECT data::text FROM analytics_event_facts WHERE tenant_id=$1 AND generation_id=$2 AND event_id=$3",tenant,generation,canonical.ID).Scan(&projected);err!=nil { t.Fatal(err) }
 if strings.Contains(projected,"phone") { t.Fatalf("unexpected sensitive field in fact: %s",projected) }
 var quarantinePayload string
 if err=pool.QueryRow(ctx,"SELECT payload_sha256 FROM analytics_event_quarantine WHERE tenant_id=$1 AND generation_id=$2 AND outbox_event_id=$3",tenant,generation,unsafeOutboxID).Scan(&quarantinePayload);err!=nil { t.Fatal(err) }
 if len(quarantinePayload)!=64 { t.Fatalf("quarantine must store only a SHA-256 fingerprint: %q",quarantinePayload) }
 if _,err=pool.Exec(ctx,"UPDATE analytics_event_facts SET traffic_class='test' WHERE tenant_id=$1 AND generation_id=$2 AND event_id=$3",tenant,generation,canonical.ID);err==nil { t.Fatal("analytics facts must be immutable") }
 summary,err:=analytics.QuerySummary(ctx,pool,tenant,analytics.SummaryFilter{From:time.Now().UTC().Add(-time.Minute),To:time.Now().UTC().Add(time.Minute),MarketCode:"OM"})
 if err!=nil||summary.OrdersCreated!=1||summary.PendingEvents!=0||!summary.DataReady { t.Fatalf("unexpected active-generation summary: %+v err=%v",summary,err) }
 if applied,processErr:=consumer.Process(ctx,outbox.Event{ID:canonicalOutboxID,AggregateType:"order",AggregateID:orderID,EventType:"order.created",Payload:mustJSON(t,canonical)});processErr!=nil||applied { t.Fatalf("duplicate event must be idempotent: applied=%v err=%v",applied,processErr) }
}

func mustJSON(t *testing.T,value any) []byte {
 t.Helper()
 raw,err:=json.Marshal(value)
 if err!=nil { t.Fatal(err) }
 return raw
}
