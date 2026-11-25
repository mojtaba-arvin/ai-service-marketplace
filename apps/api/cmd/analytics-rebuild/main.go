// Command analytics-rebuild builds a complete shadow generation from canonical
// tenant-scoped outbox events, validates it, and atomically switches the pointer.
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

func required(name string) string {
 value:=strings.TrimSpace(os.Getenv(name))
 if value=="" { log.Fatalf("%s is required",name) }
 return value
}

func loadPending(ctx context.Context,db *pgxpool.Pool,tenant,consumer string,limit int)([]outbox.Event,error) {
 rows,err:=db.Query(ctx,`SELECT e.id,e.aggregate_type,e.aggregate_id,e.event_type,e.payload,e.attempts
 FROM outbox_events e
 WHERE e.payload->>'tenant_id'=$1
 AND NOT EXISTS(SELECT 1 FROM processed_events p WHERE p.consumer_name=$2 AND p.event_id=e.id)
 ORDER BY e.id LIMIT $3`,tenant,consumer,limit)
 if err!=nil{return nil,err}
 defer rows.Close()
 events:=make([]outbox.Event,0,limit)
 for rows.Next(){
  var event outbox.Event
  if err=rows.Scan(&event.ID,&event.AggregateType,&event.AggregateID,&event.EventType,&event.Payload,&event.Attempts);err!=nil{return nil,err}
  events=append(events,event)
 }
 return events,rows.Err()
}

type validation struct { Source,Processed,Facts,Quarantined,Pending int64 }

func validateGeneration(ctx context.Context,db *pgxpool.Pool,tenant,generation,consumer string)(validation,error) {
 var v validation
 err:=db.QueryRow(ctx,`SELECT
 (SELECT count(*) FROM outbox_events WHERE payload->>'tenant_id'=$1),
 (SELECT count(*) FROM processed_events WHERE consumer_name=$3),
 (SELECT count(*) FROM analytics_event_facts WHERE tenant_id=$1 AND generation_id=$2),
 (SELECT count(*) FROM analytics_event_quarantine WHERE tenant_id=$1 AND generation_id=$2),
 (SELECT count(*) FROM outbox_events e WHERE e.payload->>'tenant_id'=$1
  AND NOT EXISTS(SELECT 1 FROM processed_events p WHERE p.consumer_name=$3 AND p.event_id=e.id))`,
 tenant,generation,consumer).Scan(&v.Source,&v.Processed,&v.Facts,&v.Quarantined,&v.Pending)
 if err!=nil{return v,err}
 if v.Pending!=0||v.Source!=v.Processed||v.Processed!=v.Facts+v.Quarantined {
  return v,fmt.Errorf("generation validation failed: source=%d processed=%d facts=%d quarantined=%d pending=%d",v.Source,v.Processed,v.Facts,v.Quarantined,v.Pending)
 }
 // Every source row must have exactly one durable outcome (fact or quarantine).
 var missing int64
 err=db.QueryRow(ctx,`SELECT count(*) FROM outbox_events e
 WHERE e.payload->>'tenant_id'=$1
 AND NOT EXISTS(SELECT 1 FROM analytics_event_facts f WHERE f.tenant_id=$1 AND f.generation_id=$2 AND f.outbox_event_id=e.id)
 AND NOT EXISTS(SELECT 1 FROM analytics_event_quarantine q WHERE q.tenant_id=$1 AND q.generation_id=$2 AND q.outbox_event_id=e.id)`,tenant,generation).Scan(&missing)
 if err!=nil{return v,err}
 if missing!=0{return v,fmt.Errorf("generation validation found %d source events without a fact or quarantine record",missing)}
 return v,nil
}

// cutover serializes competing rebuilds and switches generation state and the
// tenant pointer in one transaction. No sequence-ID watermark is used: a
// producer that commits after this snapshot is picked up by the new live worker.
func cutover(ctx context.Context,db *pgxpool.Pool,tenant,generation,consumer string)error {
 tx,err:=db.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.Serializable})
 if err!=nil{return err}
 defer tx.Rollback(ctx)
 var current string
 err=tx.QueryRow(ctx,`SELECT generation_id FROM analytics_active_generations WHERE tenant_id=$1 FOR UPDATE`,tenant).Scan(&current)
 if err!=nil&&!errors.Is(err,pgx.ErrNoRows){return err}
 var status,recordedConsumer string
 if err=tx.QueryRow(ctx,`SELECT status,consumer_name FROM analytics_projection_generations WHERE tenant_id=$1 AND generation_id=$2 FOR UPDATE`,tenant,generation).Scan(&status,&recordedConsumer);err!=nil{return err}
 if recordedConsumer!=consumer||status!="building"{return fmt.Errorf("target generation identity/status changed: consumer=%q status=%q",recordedConsumer,status)}
 var pending int64
 if err=tx.QueryRow(ctx,`SELECT count(*) FROM outbox_events e WHERE e.payload->>'tenant_id'=$1
 AND NOT EXISTS(SELECT 1 FROM processed_events p WHERE p.consumer_name=$2 AND p.event_id=e.id)`,tenant,consumer).Scan(&pending);err!=nil{return err}
 if pending!=0{return fmt.Errorf("refusing cutover with %d committed events still pending",pending)}
 if current!="" {
  if current==generation{return errors.New("target generation is already active")}
  if _,err=tx.Exec(ctx,`UPDATE analytics_projection_generations SET status='retired',retired_at=now()
   WHERE tenant_id=$1 AND generation_id=$2 AND status='active'`,tenant,current);err!=nil{return err}
 }
 if _,err=tx.Exec(ctx,`UPDATE analytics_projection_generations SET status='active',activated_at=now()
 WHERE tenant_id=$1 AND generation_id=$2 AND status='building'`,tenant,generation);err!=nil{return err}
 if current=="" {
  _,err=tx.Exec(ctx,`INSERT INTO analytics_active_generations(tenant_id,generation_id) VALUES($1,$2)`,tenant,generation)
 } else {
  _,err=tx.Exec(ctx,`UPDATE analytics_active_generations SET generation_id=$2,activated_at=now() WHERE tenant_id=$1`,tenant,generation)
 }
 if err!=nil{return err}
 return tx.Commit(ctx)
}


func rollbackCutover(ctx context.Context,db *pgxpool.Pool,tenant,target,consumer string)error {
 tx,err:=db.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.Serializable})
 if err!=nil{return err}
 defer tx.Rollback(ctx)
 var current string
 if err=tx.QueryRow(ctx,`SELECT generation_id FROM analytics_active_generations WHERE tenant_id=$1 FOR UPDATE`,tenant).Scan(&current);err!=nil{return err}
 if current==target{return errors.New("rollback target is already active")}
 var status,recordedConsumer string
 if err=tx.QueryRow(ctx,`SELECT status,consumer_name FROM analytics_projection_generations WHERE tenant_id=$1 AND generation_id=$2 FOR UPDATE`,tenant,target).Scan(&status,&recordedConsumer);err!=nil{return err}
 if status!="retired"||recordedConsumer!=consumer{return fmt.Errorf("rollback target must be a retained retired generation: status=%q consumer=%q",status,recordedConsumer)}
 var pending int64
 if err=tx.QueryRow(ctx,`SELECT count(*) FROM outbox_events e WHERE e.payload->>'tenant_id'=$1
 AND NOT EXISTS(SELECT 1 FROM processed_events p WHERE p.consumer_name=$2 AND p.event_id=e.id)`,tenant,consumer).Scan(&pending);err!=nil{return err}
 if pending!=0{return fmt.Errorf("refusing rollback with %d committed events still pending",pending)}
 if _,err=tx.Exec(ctx,`UPDATE analytics_projection_generations SET status='retired',retired_at=now()
 WHERE tenant_id=$1 AND generation_id=$2 AND status='active'`,tenant,current);err!=nil{return err}
 if _,err=tx.Exec(ctx,`UPDATE analytics_projection_generations SET status='active',retired_at=NULL
 WHERE tenant_id=$1 AND generation_id=$2 AND status='retired'`,tenant,target);err!=nil{return err}
 if _,err=tx.Exec(ctx,`UPDATE analytics_active_generations SET generation_id=$2,activated_at=now() WHERE tenant_id=$1`,tenant,target);err!=nil{return err}
 return tx.Commit(ctx)
}

func runRollback(ctx context.Context,pool *pgxpool.Pool,tenant,target string)error {
 consumerName:="analytics:"+tenant+":"+target
 var status,recordedConsumer string
 if err:=pool.QueryRow(ctx,`SELECT status,consumer_name FROM analytics_projection_generations WHERE tenant_id=$1 AND generation_id=$2`,tenant,target).Scan(&status,&recordedConsumer);err!=nil{return err}
 if status!="retired"||recordedConsumer!=consumerName{return fmt.Errorf("rollback target is not a retained generation: status=%q consumer=%q",status,recordedConsumer)}
 consumer:=outbox.Consumer{DB:pool,Name:consumerName,Handler:(analytics.Projector{DB:pool,ConsumerName:consumerName,GenerationID:target,TenantID:tenant}).Handle}
 for {
  if err:=ctx.Err();err!=nil{return err}
  batch,err:=loadPending(ctx,pool,tenant,consumerName,100)
  if err!=nil{return err}
  if len(batch)==0{break}
  for _,event:=range batch{if _,err=consumer.Process(ctx,event);err!=nil{return fmt.Errorf("catch up rollback generation at event %d: %w",event.ID,err)}}
 }
 validated,err:=validateGeneration(ctx,pool,tenant,target,consumerName)
 if err!=nil{return fmt.Errorf("rollback generation validation: %w",err)}
 log.Printf("rollback generation caught up: source=%d facts=%d quarantined=%d",validated.Source,validated.Facts,validated.Quarantined)
 if err=rollbackCutover(ctx,pool,tenant,target,consumerName);err!=nil{return fmt.Errorf("atomic analytics rollback: %w",err)}
 log.Printf("analytics generation %q restored for tenant %q; restart analytics-worker with the restored generation ID",target,tenant)
 return nil
}


func lockTenant(ctx context.Context,pool *pgxpool.Pool,tenant string)(func(),error) {
 conn,err:=pool.Acquire(ctx)
 if err!=nil{return nil,err}
 if _,err=conn.Exec(ctx,`SELECT pg_advisory_lock(hashtextextended($1,0))`,tenant);err!=nil{conn.Release();return nil,err}
 release:=func(){
  unlockCtx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
  defer cancel()
  if _,unlockErr:=conn.Exec(unlockCtx,`SELECT pg_advisory_unlock(hashtextextended($1,0))`,tenant);unlockErr!=nil{log.Printf("WARNING: release analytics rebuild lock: %v",unlockErr)}
  conn.Release()
 }
 return release,nil
}

func run(ctx context.Context)error {
 dsn:=required("DATABASE_URL")
 tenant:=required("ANALYTICS_TENANT_ID")
 generation:=required("ANALYTICS_GENERATION_ID")
 confirmation:=required("ANALYTICS_REBUILD_CONFIRM")
 if confirmation!=tenant+":"+generation{return errors.New("ANALYTICS_REBUILD_CONFIRM must exactly equal ANALYTICS_TENANT_ID:ANALYTICS_GENERATION_ID")}
 if len(tenant)>120||len(generation)>120{return errors.New("analytics tenant and generation IDs must be at most 120 characters")}
 consumerName:="analytics:"+tenant+":"+generation
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{return fmt.Errorf("configure database pool: %w",err)}
 defer pool.Close()
 if err=pool.Ping(ctx);err!=nil{return fmt.Errorf("database unavailable: %w",err)}
 // A failed run can resume the same immutable building generation. An active
 // or retired generation is never reused as a rebuild target.
 tx,err:=pool.BeginTx(ctx,pgx.TxOptions{})
 if err!=nil{return err}
 var existingStatus,existingConsumer string
 err=tx.QueryRow(ctx,`SELECT status,consumer_name FROM analytics_projection_generations WHERE tenant_id=$1 AND generation_id=$2 FOR UPDATE`,tenant,generation).Scan(&existingStatus,&existingConsumer)
 if errors.Is(err,pgx.ErrNoRows){
  _,err=tx.Exec(ctx,`INSERT INTO analytics_projection_generations(tenant_id,generation_id,consumer_name,status) VALUES($1,$2,$3,'building')`,tenant,generation,consumerName)
 } else if err==nil {
  if existingStatus!="building"||existingConsumer!=consumerName{_ = tx.Rollback(ctx);return fmt.Errorf("generation already exists and cannot be reused: status=%q consumer=%q",existingStatus,existingConsumer)}
 } 
 if err!=nil{_ = tx.Rollback(ctx);return err}
 if err=tx.Commit(ctx);err!=nil{return err}

 consumer:=outbox.Consumer{DB:pool,Name:consumerName,Handler:(analytics.Projector{DB:pool,ConsumerName:consumerName,GenerationID:generation,TenantID:tenant}).Handle}
 totalApplied:=0
 for {
  if err=ctx.Err();err!=nil{return err}
  batch,batchErr:=loadPending(ctx,pool,tenant,consumerName,100)
  if batchErr!=nil{return fmt.Errorf("load rebuild batch: %w",batchErr)}
  if len(batch)==0{break}
  for _,event:=range batch{
   applied,processErr:=consumer.Process(ctx,event)
   if processErr!=nil{return fmt.Errorf("project outbox event %d: %w",event.ID,processErr)}
   if applied{totalApplied++}
  }
  if totalApplied>0&&totalApplied%1000==0{log.Printf("analytics rebuild progress: %d events processed",totalApplied)}
 }
 validated,err:=validateGeneration(ctx,pool,tenant,generation,consumerName)
 if err!=nil{return err}
 log.Printf("analytics rebuild validated: source=%d facts=%d quarantined=%d",validated.Source,validated.Facts,validated.Quarantined)
 if err=cutover(ctx,pool,tenant,generation,consumerName);err!=nil{return fmt.Errorf("atomic analytics generation cutover: %w",err)}
 log.Printf("analytics generation %q is now active for tenant %q; restart analytics-worker with the new generation ID to resume live tail",generation,tenant)
 return nil
}

func main(){
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
 defer cancel()
 if err:=run(ctx);err!=nil{log.Fatal(err)}
}
