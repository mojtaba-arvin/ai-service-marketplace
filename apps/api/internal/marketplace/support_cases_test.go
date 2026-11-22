package marketplace

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

func TestSupportCaseLifecycleAndTenantPrivacyPostgres(t *testing.T){
 dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
 for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","021_support_cases.sql"}{
  raw,e:=os.ReadFile(filepath.Join("..","..","migrations",name));if e!=nil{t.Fatal(e)}
  if _,e=execMigrationScript(ctx, pool, raw);e!=nil{t.Fatalf("apply %s: %v",name,e)}
 }
 store:=&Store{DB:pool}
 requester:=newTestIdentity(t,ctx,pool);agent:=newTestIdentity(t,ctx,pool);outsider:=newTestIdentity(t,ctx,pool)
 tenant:="support-tenant-"+newTestID(t)
 if _,err=pool.Exec(ctx,`INSERT INTO support_agents(tenant_id,identity_id) VALUES($1,$2)`,tenant,agent);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO support_agents(tenant_id,identity_id,active) VALUES($1,$2,false)`,tenant,outsider);err!=nil{t.Fatal(err)}
 customer:=Actor{SubjectID:requester,TenantID:tenant,Role:RoleCustomer}
 support:=Actor{SubjectID:agent,TenantID:tenant,Role:RoleSupport}
 created,err:=store.CreateSupportCase(ctx,customer,"billing","high","Refund status","Please review the refund timeline.",time.Now().Add(4*time.Hour))
 if err!=nil{t.Fatal(err)}
 if created.Status!="open"||created.SLADueAt.IsZero(){t.Fatalf("unexpected case: %+v",created)}
 inactiveSupport:=Actor{SubjectID:outsider,TenantID:tenant,Role:RoleSupport}
 if _,err=store.GetSupportCase(ctx,inactiveSupport,created.ID);!errors.Is(err,ErrForbidden){t.Fatalf("inactive support agent read: %v",err)}
 if _,err=store.AssignSupportCase(ctx,inactiveSupport,created.ID,agent,"unauthorized assignment");!errors.Is(err,ErrForbidden){t.Fatalf("inactive support agent assignment: %v",err)}
 if _,err=store.TransitionSupportCase(ctx,inactiveSupport,created.ID,"in_progress","unauthorized transition");!errors.Is(err,ErrForbidden){t.Fatalf("inactive support agent transition: %v",err)}
 if _,err=store.GetSupportCase(ctx,Actor{SubjectID:outsider,TenantID:tenant,Role:RoleCustomer},created.ID);!errors.Is(err,ErrForbidden){t.Fatalf("unrelated identity read: %v",err)}
 assigned,err:=store.AssignSupportCase(ctx,support,created.ID,agent,"Taking ownership")
 if err!=nil{t.Fatal(err)}
 if assigned.AssignedSupportIdentityID==nil||*assigned.AssignedSupportIdentityID!=agent{t.Fatalf("assignment missing: %+v",assigned)}
 moved,err:=store.TransitionSupportCase(ctx,support,created.ID,"in_progress","Review started")
 if err!=nil||moved.Status!="in_progress"{t.Fatalf("transition: %+v %v",moved,err)}
 moved,err=store.TransitionSupportCase(ctx,support,created.ID,"resolved","Resolution sent")
 if err!=nil||moved.Status!="resolved"{t.Fatalf("resolve: %+v %v",moved,err)}
 moved,err=store.TransitionSupportCase(ctx,support,created.ID,"closed","Customer confirmed")
 if err!=nil||moved.Status!="closed"||moved.ClosedAt==nil{t.Fatalf("close: %+v %v",moved,err)}
 if _,err=store.TransitionSupportCase(ctx,support,created.ID,"open","reopen");!errors.Is(err,ErrSupportCaseState){t.Fatalf("invalid transition: %v",err)}
 var events int
 if err=pool.QueryRow(ctx,`SELECT count(*) FROM support_case_events WHERE case_id=$1`,created.ID).Scan(&events);err!=nil{t.Fatal(err)}
 if events!=5{t.Fatalf("expected create, assign, and three transitions; got %d",events)}
}
