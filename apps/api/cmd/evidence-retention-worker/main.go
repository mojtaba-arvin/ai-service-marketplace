package main

import (
 "context"
 "log"
 "os"
 "os/signal"
 "strconv"
 "syscall"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketplace"
)
func envInt(name string,def,min,max int)int{raw:=os.Getenv(name);if raw==""{return def};v,e:=strconv.Atoi(raw);if e!=nil||v<min||v>max{log.Fatalf("%s is outside allowed bounds",name)};return v}
func main(){
 dsn:=os.Getenv("DATABASE_URL");if dsn==""{log.Fatal("DATABASE_URL is required")}
 evidence,e:=marketplace.NewEvidenceServiceFromEnv();if e!=nil{log.Fatalf("configure private evidence storage: %v",e)}
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop()
 pool,e:=pgxpool.New(ctx,dsn);if e!=nil{log.Fatal("configure database connection")};defer pool.Close()
 if e=pool.Ping(ctx);e!=nil{log.Fatal("evidence retention database unavailable")}
 interval:=time.Duration(envInt("EVIDENCE_RETENTION_POLL_SECONDS",30,1,3600))*time.Second
 worker:=&marketplace.EvidenceDeletionWorker{DB:pool,Storage:evidence.Storage,BatchSize:envInt("EVIDENCE_RETENTION_BATCH_SIZE",25,1,500),
 Lease:time.Duration(envInt("EVIDENCE_RETENTION_LEASE_SECONDS",60,1,900))*time.Second,WorkerID:"evidence-retention-"+time.Now().UTC().Format("20060102150405.000000000")}
 ticker:=time.NewTicker(interval);defer ticker.Stop()
 for{if _,e=worker.RunOnce(ctx);e!=nil&&ctx.Err()==nil{log.Printf("evidence retention cycle failed (category=worker_failure)")}
 select{case<-ctx.Done():return;case<-ticker.C:}}
}
