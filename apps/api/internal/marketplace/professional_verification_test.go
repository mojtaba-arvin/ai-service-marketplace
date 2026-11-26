package marketplace

import (
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

func TestProfessionalVerificationCasePostgres(t *testing.T){
 dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
 for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","006_provider_eligibility.sql","020_professional_verification.sql"}{
  raw,e:=os.ReadFile(filepath.Join("..","..","migrations",name));if e!=nil{t.Fatal(e)}
  if _,e=execMigrationScript(ctx, pool, raw);e!=nil{t.Fatalf("apply %s: %v",name,e)}
 }
 store:=&Store{DB:pool}
 professionalID,operatorID,providerID:=newTestIdentity(t,ctx,pool),newTestIdentity(t,ctx,pool),newTestID(t)
 tenant:="tenant-verification-"+providerID
 if _,err=pool.Exec(ctx,`INSERT INTO provider_profiles(id,identity_id,tenant_id,display_name,active) VALUES($1,$2,$3,'Test professional',true)`,providerID,professionalID,tenant);err!=nil{t.Fatal(err)}
 professional:=Actor{SubjectID:professionalID,TenantID:tenant,Role:RoleProfessional}
 operator:=Actor{SubjectID:operatorID,TenantID:tenant,Role:RoleOperator}
 if err=ensureTestOperatorGrants(ctx,pool,operator,ActionProviderVerify,ActionCaseRead);err!=nil{t.Fatal(err)}
 created,err:=store.CreateProfessionalVerificationCase(ctx,professional,providerID,"private://evidence/"+providerID)
 if err!=nil{t.Fatal(err)}
 if created.Status!="submitted"||created.EvidenceRef==""{t.Fatalf("unexpected case: %+v",created)}
 if _,err=store.CreateProfessionalVerificationCase(ctx,professional,providerID,"private://evidence/second-"+providerID);!errors.Is(err,ErrVerificationAlreadyExists){t.Fatalf("duplicate open case: %v",err)}
 hidden,err:=store.GetProfessionalVerificationCase(ctx,professional,created.ID);if err!=nil{t.Fatal(err)}
 if hidden.EvidenceRef!=""{t.Fatal("subject must not receive private evidence reference")}
 if _,err=store.ReviewProfessionalVerificationCase(ctx,professional,created.ID,"approved","self review");!errors.Is(err,ErrForbidden){t.Fatalf("self review should be forbidden: %v",err)}
 inReview,err:=store.StartProfessionalVerificationReview(ctx,operator,created.ID,"Review started")
 if err!=nil{t.Fatal(err)}
 if inReview.Status!="in_review"{t.Fatalf("case did not enter review: %+v",inReview)}
 if _,err=store.StartProfessionalVerificationReview(ctx,operator,created.ID,"Duplicate review");!errors.Is(err,ErrVerificationState){t.Fatalf("duplicate review transition: %v",err)}
 approved,err:=store.ReviewProfessionalVerificationCase(ctx,operator,created.ID,"approved","Documents reviewed")
 if err!=nil{t.Fatal(err)}
 if approved.Status!="approved"||approved.ReviewerIdentityID==nil||approved.DecidedAt==nil{t.Fatalf("unexpected decision: %+v",approved)}
 visible,err:=store.GetProfessionalVerificationCase(ctx,operator,created.ID);if err!=nil{t.Fatal(err)}
 if visible.EvidenceRef==""{t.Fatal("operator should be able to access evidence pointer")}
 var verified bool
 if err=pool.QueryRow(ctx,`SELECT verified_at IS NOT NULL FROM provider_profiles WHERE id=$1`,providerID).Scan(&verified);err!=nil{t.Fatal(err)}
 if !verified{t.Fatal("approval did not verify provider")}
 var fromStatus,toStatus string
 if err=pool.QueryRow(ctx,`SELECT from_status,to_status FROM professional_verification_events WHERE case_id=$1 ORDER BY id DESC LIMIT 1`,created.ID).Scan(&fromStatus,&toStatus);err!=nil{t.Fatal(err)}
 if fromStatus!="in_review"||toStatus!="approved"{t.Fatalf("review audit transition = %s -> %s",fromStatus,toStatus)}
 otherTenant:=Actor{SubjectID:newTestIdentity(t,ctx,pool),TenantID:"other-tenant",Role:RoleOperator}
 if _,err=store.GetProfessionalVerificationCase(ctx,otherTenant,created.ID);!errors.Is(err,ErrForbidden){t.Fatalf("cross-tenant read: %v",err)}
}

func newTestIdentity(t *testing.T,ctx context.Context,pool *pgxpool.Pool)string{
 t.Helper();id:=newTestID(t);phone:=fmt.Sprintf("+1997%011d",time.Now().UnixNano()%100000000000)
 if _,err:=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,id,phone);err!=nil{t.Fatal(err)}
 return id
}
func newTestID(t *testing.T)string{t.Helper();id,err:=newID();if err!=nil{t.Fatal(err)};return id}
