package main

import (
 "context"
 "errors"
 "fmt"
 "log"
 "os"
 "os/signal"
 "strings"
 "syscall"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/analytics"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func required(name string) string { value:=strings.TrimSpace(os.Getenv(name));if value==""{log.Fatalf("%s is required",name)};return value }

func ensureActiveGeneration(ctx context.Context,db *pgxpool.Pool,tenantID,generationID,consumerName string) error {
 tx,err:=db.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return err};defer tx.Rollback(ctx)
 if _,err=tx.Exec(ctx,`INSERT INTO analytics_projection_generations(tenant_id,generation_id,consumer_name,status) VALUES($1,$2,$3,'building') ON CONFLICT(tenant_id,generation_id) DO NOTHING`,tenantID,generationID,consumerName);err!=nil{return err}
 var active string
 err=tx.QueryRow(ctx,`SELECT generation_id FROM analytics_active_generations WHERE tenant_id=$1 FOR UPDATE`,tenantID).Scan(&active)
 if errors.Is(err,pgx.ErrNoRows){if _,err=tx.Exec(ctx,`INSERT INTO analytics_active_generations(tenant_id,generation_id) VALUES($1,$2) ON CONFLICT(tenant_id) DO NOTHING`,tenantID,generationID);err!=nil{return err};err=tx.QueryRow(ctx,`SELECT generation_id FROM analytics_active_generations WHERE tenant_id=$1`,tenantID).Scan(&active)}
 if err!=nil{return err};if active!=generationID{return fmt.Errorf("generation %q is not active for tenant %q (active=%q); refusing to consume",generationID,tenantID,active)}
 var recordedConsumer,status string
 if err=tx.QueryRow(ctx,`SELECT consumer_name,status FROM analytics_projection_generations WHERE tenant_id=$1 AND generation_id=$2`,tenantID,generationID).Scan(&recordedConsumer,&status);err!=nil{return err}
 if recordedConsumer!=consumerName||status=="retired"{return fmt.Errorf("analytics generation identity/status mismatch: consumer=%q status=%q",recordedConsumer,status)}
 if _,err=tx.Exec(ctx,`UPDATE analytics_projection_generations SET status='active',activated_at=COALESCE(activated_at,now()) WHERE tenant_id=$1 AND generation_id=$2 AND status='building'`,tenantID,generationID);err!=nil{return err}
 return tx.Commit(ctx)
}

func loadBatch(ctx context.Context,db *pgxpool.Pool,tenantID,consumerName string,limit int)([]outbox.Event,error){
 rows,err:=db.Query(ctx,`SELECT e.id,e.aggregate_type,e.aggregate_id,e.event_type,e.payload,e.attempts FROM outbox_events e WHERE e.payload->>'tenant_id'=$1 AND NOT EXISTS(SELECT 1 FROM processed_events p WHERE p.consumer_name=$2 AND p.event_id=e.id) ORDER BY e.id LIMIT $3`,tenantID,consumerName,limit)
 if err!=nil{return nil,err};defer rows.Close();events:=make([]outbox.Event,0,limit)
 for rows.Next(){var e outbox.Event;if err=rows.Scan(&e.ID,&e.AggregateType,&e.AggregateID,&e.EventType,&e.Payload,&e.Attempts);err!=nil{return nil,err};events=append(events,e)}
 return events,rows.Err()
}

func main(){
 dsn:=required("DATABASE_URL");tenant:=required("ANALYTICS_TENANT_ID");generation:=required("ANALYTICS_GENERATION_ID");workerID:=required("ANALYTICS_WORKER_ID")
 if len(tenant)>120||len(generation)>120||len(workerID)>120{log.Fatal("analytics tenant, generation and worker IDs must be at most 120 characters")}
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{log.Fatalf("configure database pool: %v",err)};defer pool.Close()
 if err=pool.Ping(ctx);err!=nil{log.Fatalf("database unavailable: %v",err)}
 consumerName:=fmt.Sprintf("analytics:%s:%s",tenant,generation)
 if err=ensureActiveGeneration(ctx,pool,tenant,generation,consumerName);err!=nil{log.Fatalf("activate initial analytics generation: %v",err)}
 consumer:=outbox.Consumer{DB:pool,Name:consumerName,Handler:(analytics.Projector{DB:pool,ConsumerName:consumerName,GenerationID:generation,TenantID:tenant}).Handle}
 ticker:=time.NewTicker(time.Second);defer ticker.Stop()
 log.Printf("analytics worker %q started for tenant %q generation %q",workerID,tenant,generation)
 for{
  events,batchErr:=loadBatch(ctx,pool,tenant,consumerName,100)
  failed:=batchErr!=nil
  if batchErr!=nil{log.Printf("analytics batch query failed: %v",batchErr)}else{
   for _,event:=range events{if ctx.Err()!=nil{break};if _,err=consumer.Process(ctx,event);err!=nil{log.Printf("analytics event %d processing failed: %v",event.ID,err);failed=true;break}}
  }
  if len(events)==100&&!failed{continue}
  select{case <-ctx.Done():log.Printf("analytics worker %q stopping",workerID);return;case <-ticker.C:}
 }
}
