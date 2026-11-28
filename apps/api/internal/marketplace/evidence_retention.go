package marketplace

import (
 "context"
 "errors"
 "os"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

// EvidenceDeletionWorker processes only explicitly expired, non-held evidence.
type EvidenceDeletionWorker struct {
 DB *pgxpool.Pool
 Storage PrivateEvidenceStorage
 BatchSize int
 Lease time.Duration
 WorkerID string
 Now func() time.Time
}
type evidenceDeletionClaim struct { id,tenant,report,token string }

func (w *EvidenceDeletionWorker) valid() error {
 if w==nil||w.DB==nil||w.Storage==nil||w.WorkerID=="" { return errors.New("evidence deletion worker is not configured") }
 if w.BatchSize<1||w.BatchSize>500||w.Lease<time.Second||w.Lease>15*time.Minute { return errors.New("evidence deletion worker bounds are invalid") }
 return nil
}

// RunOnce persists claims/audit before object-store I/O. Claim tokens fence stale
// workers; storage deletion is idempotent and an absent object is successful.
func (w *EvidenceDeletionWorker) RunOnce(ctx context.Context)(int,error) {
 if err:=w.valid();err!=nil{return 0,err}
 now:=time.Now().UTC();if w.Now!=nil{now=w.Now().UTC()}
 tx,err:=w.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return 0,err};defer tx.Rollback(ctx)
 rows,err:=tx.Query(ctx,`SELECT id,tenant_id,report_id FROM incident_evidence
 WHERE legal_hold=false AND ((deletion_state='active' AND retention_expires_at IS NOT NULL AND retention_expires_at<=$1)
 OR (deletion_state='deletion_pending' AND (deletion_claimed_until IS NULL OR deletion_claimed_until<=$1)))
 ORDER BY retention_expires_at NULLS LAST,created_at,id FOR UPDATE SKIP LOCKED LIMIT $2`,now,w.BatchSize)
 if err!=nil{return 0,err}
 claims:=make([]evidenceDeletionClaim,0,w.BatchSize)
 for rows.Next(){var c evidenceDeletionClaim;if err=rows.Scan(&c.id,&c.tenant,&c.report);err!=nil{rows.Close();return 0,err}
  c.token,err=newID();if err!=nil{rows.Close();return 0,err};claims=append(claims,c)}
 if err=rows.Err();err!=nil{rows.Close();return 0,err};rows.Close()
 for _,c:=range claims{
  _,err=tx.Exec(ctx,`UPDATE incident_evidence SET deletion_state='deletion_pending',deletion_claim_token=$2,deletion_claimed_until=$3 WHERE id=$1`,c.id,c.token,now.Add(w.Lease));if err!=nil{return 0,err}
  _,err=tx.Exec(ctx,`INSERT INTO incident_evidence_access_events(tenant_id,report_id,evidence_id,actor_identity_id,action,outcome,reason_category)
  VALUES($1,$2,$3,'system:evidence-retention','deletion_claimed','success','retention_expired')`,c.tenant,c.report,c.id);if err!=nil{return 0,err}
 }
 if err=tx.Commit(ctx);err!=nil{return 0,err}
 done:=0
 for _,c:=range claims{
  if err=ctx.Err();err!=nil{return done,err}
  deleteErr:=w.Storage.Delete(ctx,c.id)
  missing:=errors.Is(deleteErr,os.ErrNotExist)
  if deleteErr==nil||missing{
   tag,e:=w.DB.Exec(ctx,`UPDATE incident_evidence SET deletion_state='deleted',deleted_at=COALESCE(deleted_at,now()),
    deletion_claim_token=NULL,deletion_claimed_until=NULL,deletion_error_category=''
    WHERE id=$1 AND deletion_state='deletion_pending' AND deletion_claim_token=$2 AND legal_hold=false`,c.id,c.token)
   if e!=nil{return done,e};if tag.RowsAffected()!=1{return done,errors.New("evidence deletion claim lost")}
   reason:="retention_expired";if missing{reason="missing_object"}
   _,e=w.DB.Exec(ctx,`INSERT INTO incident_evidence_access_events(tenant_id,report_id,evidence_id,actor_identity_id,action,outcome,reason_category)
    VALUES($1,$2,$3,'system:evidence-retention','deletion_succeeded','success',$4)`,c.tenant,c.report,c.id,reason);if e!=nil{return done,e}
  }else{
   _,e:=w.DB.Exec(ctx,`UPDATE incident_evidence SET deletion_attempts=LEAST(deletion_attempts+1,2147483647),
    deletion_claim_token=NULL,deletion_claimed_until=NULL,deletion_error_category='storage_failure'
    WHERE id=$1 AND deletion_state='deletion_pending' AND deletion_claim_token=$2`,c.id,c.token)
   if e!=nil{return done,e}
   _,e=w.DB.Exec(ctx,`INSERT INTO incident_evidence_access_events(tenant_id,report_id,evidence_id,actor_identity_id,action,outcome,reason_category)
    VALUES($1,$2,$3,'system:evidence-retention','deletion_failed','failure','deletion_retry')`,c.tenant,c.report,c.id);if e!=nil{return done,e}
  }
  done++
 }
 return done,nil
}

// ReconcileMissing finalizes only pending tombstones whose object is absent.
// Active evidence is never silently removed by a reconciliation pass.
func (w *EvidenceDeletionWorker) ReconcileMissing(ctx context.Context,limit int)(int,error){
 if err:=w.valid();err!=nil{return 0,err};if limit<1||limit>w.BatchSize{return 0,errors.New("invalid reconciliation limit")}
 rows,err:=w.DB.Query(ctx,`SELECT id,tenant_id,report_id FROM incident_evidence WHERE deletion_state='deletion_pending' AND legal_hold=false ORDER BY created_at,id LIMIT $1`,limit)
 if err!=nil{return 0,err}
 type item struct{id,tenant,report string};items:=make([]item,0,limit)
 for rows.Next(){var v item;if err=rows.Scan(&v.id,&v.tenant,&v.report);err!=nil{rows.Close();return 0,err};items=append(items,v)}
 if err=rows.Err();err!=nil{rows.Close();return 0,err};rows.Close();count:=0
 for _,v:=range items{
  _,e:=w.Storage.Get(ctx,v.id,1);if !errors.Is(e,os.ErrNotExist){continue}
  tx,e:=w.DB.BeginTx(ctx,pgx.TxOptions{});if e!=nil{return count,e}
  tag,e:=tx.Exec(ctx,`UPDATE incident_evidence SET deletion_state='deleted',deleted_at=COALESCE(deleted_at,now()),
   deletion_claim_token=NULL,deletion_claimed_until=NULL,deletion_error_category=''
   WHERE id=$1 AND tenant_id=$2 AND deletion_state='deletion_pending' AND legal_hold=false`,v.id,v.tenant)
  if e==nil&&tag.RowsAffected()==1{_,e=tx.Exec(ctx,`INSERT INTO incident_evidence_access_events(tenant_id,report_id,evidence_id,actor_identity_id,action,outcome,reason_category)
   VALUES($1,$2,$3,'system:evidence-retention','deletion_reconciled','success','reconciliation')`,v.tenant,v.report,v.id)}
  if e!=nil{_ = tx.Rollback(ctx);return count,e};if e=tx.Commit(ctx);e!=nil{return count,e};if tag.RowsAffected()==1{count++}
 }
 return count,nil
}
