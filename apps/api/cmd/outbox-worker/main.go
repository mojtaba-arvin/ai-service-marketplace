package main

import (
 "context"
 "log"
 "net/http"
 "os"
 "os/signal"
 "syscall"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func required(name string)string{
 value:=os.Getenv(name)
 if value==""{log.Fatalf("%s is required",name)}
 return value
}

func main(){
 dsn:=required("DATABASE_URL")
 workerID:=required("OUTBOX_WORKER_ID")
 publisher,err:=outbox.NewHTTPPublisher(required("OUTBOX_PUBLISH_URL"),required("OUTBOX_PUBLISH_TOKEN"),&http.Client{Timeout:10*time.Second})
 if err!=nil{log.Fatalf("configure outbox publisher: %v",err)}
 ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{log.Fatal(err)};defer pool.Close()
 if err=pool.Ping(ctx);err!=nil{log.Fatalf("database unavailable: %v",err)}
 worker:=outbox.Worker{
  Repository:&outbox.Repository{DB:pool},Publisher:publisher,WorkerID:workerID,
  BatchSize:25,Lease:60*time.Second,MaxAttempts:8,
  BaseBackoff:time.Second,MaxBackoff:2*time.Minute,Retryable:outbox.RetryableHTTP,
 }
 ticker:=time.NewTicker(time.Second);defer ticker.Stop()
 log.Printf("outbox worker %q started",workerID)
 for{
  published,runErr:=worker.RunOnce(ctx)
  if runErr!=nil{log.Printf("outbox batch failed: %v",runErr)}
  if published>0{continue}
  select{
  case <-ctx.Done():
   log.Printf("outbox worker %q stopping",workerID)
   return
  case <-ticker.C:
  }
 }
}
