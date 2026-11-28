package marketplace

import (
 "context"
 "errors"
 "fmt"
 "os"
 "sync"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

type deletionTestStorage struct{ mu sync.Mutex; objects map[string]bool; failIDs map[string]bool; calls map[string]int }
func(s *deletionTestStorage)Put(context.Context,string,[]byte)error{return nil}
func(s *deletionTestStorage)Get(_ context.Context,id string,_ int64)([]byte,error){s.mu.Lock();defer s.mu.Unlock();if !s.objects[id]{return nil,os.ErrNotExist};return []byte("ciphertext"),nil}
func(s *deletionTestStorage)Delete(_ context.Context,id string)error{s.mu.Lock();defer s.mu.Unlock();s.calls[id]++;if s.failIDs[id]{return errors.New("provider response contains secret and must not be persisted")};if !s.objects[id]{return os.ErrNotExist};delete(s.objects,id);return nil}

func TestEvidenceDeletionWorkerPostgresRetryHoldMissingAndConcurrency(t *testing.T){
 dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 schema:=fmt.Sprintf("evidence_delete_%d",time.Now().UnixNano())
 if _,err=admin.Exec(ctx,"CREATE SCHEMA "+schema);err!=nil{t.Fatal(err)}
 defer func(){_,_=admin.Exec(context.Background(),"DROP SCHEMA "+schema+" CASCADE")}()
 cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["search_path"]=schema
 db,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer db.Close()
 _,err=db.Exec(ctx,`CREATE TABLE incident_evidence(
 id text PRIMARY KEY,tenant_id text NOT NULL,report_id text NOT NULL,retention_expires_at timestamptz,
 legal_hold boolean NOT NULL,deletion_state text NOT NULL,deletion_attempts integer NOT NULL DEFAULT 0,created_at timestamptz NOT NULL DEFAULT now(),
 deletion_claim_token text,deletion_claimed_until timestamptz,deleted_at timestamptz,deletion_error_category text NOT NULL DEFAULT '')`)
 if err!=nil{t.Fatal(err)}
 _,err=db.Exec(ctx,`CREATE TABLE incident_evidence_access_events(
 id bigserial PRIMARY KEY,tenant_id text,report_id text,evidence_id text,actor_identity_id text,
 action text,outcome text,reason_category text,attempt integer NOT NULL DEFAULT 0,created_at timestamptz DEFAULT now())`);if err!=nil{t.Fatal(err)}
 now:=time.Now().UTC()
 ids:=[]string{"11111111111111111111111111111111","22222222222222222222222222222222","33333333333333333333333333333333","44444444444444444444444444444444","66666666666666666666666666666666"}
 for i,id:=range ids{
  state:="active";expiry:=any(now.Add(-time.Hour));hold:=false
  if i==1{hold=true};if i==2{expiry=nil};if i==3{state="deletion_pending";expiry=now.Add(-time.Hour)}
  _,err=db.Exec(ctx,`INSERT INTO incident_evidence(id,tenant_id,report_id,retention_expires_at,legal_hold,deletion_state) VALUES($1,'tenant-a','report-a',$2,$3,$4)`,id,expiry,hold,state);if err!=nil{t.Fatal(err)}
  if i==4{_,err=db.Exec(ctx,`UPDATE incident_evidence SET deletion_state='deletion_pending',deletion_claim_token='crashed-worker',deletion_claimed_until=$2 WHERE id=$1`,id,now.Add(-time.Hour));if err!=nil{t.Fatal(err)}}
 }
 storage:=&deletionTestStorage{objects:map[string]bool{ids[0]:true,ids[1]:true,ids[3]:true},failIDs:map[string]bool{ids[0]:true},calls:map[string]int{}}
 w:=&EvidenceDeletionWorker{DB:db,Storage:storage,BatchSize:10,Lease:time.Minute,WorkerID:"worker-a",Now:func()time.Time{return now}}
 n,err:=w.RunOnce(ctx);if err!=nil{t.Fatal(err)};if n!=3{t.Fatalf("first pass processed=%d, want 3 eligible non-held records",n)}
 var state string;var attempts int
 if err=db.QueryRow(ctx,"SELECT deletion_state,deletion_attempts FROM incident_evidence WHERE id=$1",ids[0]).Scan(&state,&attempts);err!=nil{t.Fatal(err)}
 if state!="deletion_pending"||attempts!=1{t.Fatalf("failed deletion state=%s attempts=%d",state,attempts)}
 var leaked int
 if err=db.QueryRow(ctx,"SELECT count(*) FROM incident_evidence_access_events WHERE reason_category LIKE '%secret%'").Scan(&leaked);err!=nil{t.Fatal(err)}
 if leaked!=0{t.Fatal("provider error details leaked into audit")}
 storage.mu.Lock();delete(storage.failIDs,ids[0]);storage.mu.Unlock()
 n,err=w.RunOnce(ctx);if err!=nil{t.Fatal(err)};if n!=1{t.Fatalf("retry pass processed=%d, want 1",n)}
 if err=db.QueryRow(ctx,"SELECT deletion_state FROM incident_evidence WHERE id=$1",ids[0]).Scan(&state);err!=nil{t.Fatal(err)}
 if state!="deleted"{t.Fatalf("retried deletion state=%s",state)}
 var attemptsInAudit int
 if err=db.QueryRow(ctx,"SELECT max(attempt) FROM incident_evidence_access_events WHERE evidence_id=$1",ids[0]).Scan(&attemptsInAudit);err!=nil{t.Fatal(err)}
 if attemptsInAudit!=2{t.Fatalf("retry audit attempt=%d, want 2",attemptsInAudit)}
 if err=db.QueryRow(ctx,"SELECT deletion_state FROM incident_evidence WHERE id=$1",ids[1]).Scan(&state);err!=nil{t.Fatal(err)}
 if state!="active"{t.Fatalf("legal hold must preserve object, state=%s",state)}
 if err=db.QueryRow(ctx,"SELECT deletion_state FROM incident_evidence WHERE id=$1",ids[2]).Scan(&state);err!=nil{t.Fatal(err)}
 if state!="active"{t.Fatalf("missing expiry must remain active, state=%s",state)}
 var audit int
 if err=db.QueryRow(ctx,"SELECT count(*) FROM incident_evidence_access_events WHERE action='deletion_failed' AND outcome='failure'").Scan(&audit);err!=nil{t.Fatal(err)}
 if audit!=1{t.Fatalf("failure audit count=%d, want 1",audit)}
 if err=db.QueryRow(ctx,"SELECT count(*) FROM incident_evidence_access_events WHERE action='deletion_succeeded' AND outcome='success'").Scan(&audit);err!=nil{t.Fatal(err)}
 if audit!=3{t.Fatalf("success audit count=%d, want 3 (including already absent object and retry)",audit)}
 // Two independent workers race on one pending row; SKIP LOCKED/claim tokens
 // ensure only one storage deletion is needed for the same lease window.
 _,err=db.Exec(ctx,`INSERT INTO incident_evidence(id,tenant_id,report_id,retention_expires_at,legal_hold,deletion_state) VALUES($1,'tenant-a','report-a',$2,false,'active')`, "55555555555555555555555555555555",now.Add(-time.Hour));if err!=nil{t.Fatal(err)}
 storage.mu.Lock();storage.objects["55555555555555555555555555555555"]=true;storage.mu.Unlock()
 w2:=*w;w2.WorkerID="worker-b"
 var wg sync.WaitGroup;wg.Add(2);errs:=make(chan error,2)
 go func(){defer wg.Done();_,e:=w.RunOnce(ctx);errs<-e}()
 go func(){defer wg.Done();_,e:=w2.RunOnce(ctx);errs<-e}()
 wg.Wait();close(errs);for e:=range errs{if e!=nil{t.Fatal(e)}}
 if err=db.QueryRow(ctx,"SELECT deletion_state FROM incident_evidence WHERE id=$1","55555555555555555555555555555555").Scan(&state);err!=nil{t.Fatal(err)}
 if state!="deleted"{t.Fatalf("concurrent deletion state=%s",state)}
 if storage.calls["55555555555555555555555555555555"]!=1{t.Fatalf("concurrent storage delete calls=%d, want 1",storage.calls["55555555555555555555555555555555"])}
 // Reconciliation handles an absent object left pending behind a live claim lease.
 reconcileID:="77777777777777777777777777777777"
 _,err=db.Exec(ctx,`INSERT INTO incident_evidence(id,tenant_id,report_id,retention_expires_at,legal_hold,deletion_state,deletion_claim_token,deletion_claimed_until) VALUES($1,'tenant-a','report-a',$2,false,'deletion_pending','stale-claim',$3)`,reconcileID,now.Add(-time.Hour),now.Add(time.Hour));if err!=nil{t.Fatal(err)}
 reconciled,err:=w.ReconcileMissing(ctx,1);if err!=nil{t.Fatal(err)};if reconciled!=1{t.Fatalf("reconciled=%d, want 1",reconciled)}
 if err=db.QueryRow(ctx,"SELECT deletion_state FROM incident_evidence WHERE id=$1",reconcileID).Scan(&state);err!=nil{t.Fatal(err)}
 if state!="deleted"{t.Fatalf("reconciled missing object state=%s",state)}
 if err=db.QueryRow(ctx,"SELECT count(*) FROM incident_evidence_access_events WHERE evidence_id=$1 AND action='deletion_reconciled'",reconcileID).Scan(&audit);err!=nil{t.Fatal(err)}
 if audit!=1{t.Fatalf("reconciliation audit count=%d, want 1",audit)}
}
