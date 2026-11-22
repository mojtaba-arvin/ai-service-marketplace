package marketplace

import (
 "context"
 "fmt"
 "os"
 "path/filepath"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

func TestOrganizationPropertySchemaEnforcesTenantBoundariesPostgres(t *testing.T) {
 dsn:=os.Getenv("TEST_DATABASE_URL")
 if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
 defer cancel()
 pool,err:=pgxpool.New(ctx,dsn)
 if err!=nil { t.Fatal(err) }
 defer pool.Close()
 for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","022_identity_memberships.sql","024_organizations_properties.sql"} {
  raw,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
  if readErr!=nil { t.Fatal(readErr) }
  if _,err=execMigrationScript(ctx,pool,raw);err!=nil { t.Fatalf("apply %s: %v",name,err) }
 }
 identityA,err:=newID();if err!=nil{t.Fatal(err)}
 identityB,err:=newID();if err!=nil{t.Fatal(err)}
 phoneA:=fmt.Sprintf("+1993%011d",time.Now().UnixNano()%100_000_000_000)
 phoneB:=fmt.Sprintf("+1994%011d",(time.Now().UnixNano()+1)%100_000_000_000)
 for _,entry:=range []struct{id,phone string}{{identityA,phoneA},{identityB,phoneB}} {
  if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,entry.id,entry.phone);err!=nil{t.Fatal(err)}
 }
 membershipA,err:=newID();if err!=nil{t.Fatal(err)}
 membershipB,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO identity_memberships(id,identity_id,tenant_id,role) VALUES($1,$2,'tenant-a','customer'),($3,$4,'tenant-b','customer')`,membershipA,identityA,membershipB,identityB);err!=nil{t.Fatal(err)}
 orgA,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organizations(id,tenant_id,name,slug,created_by_identity_id) VALUES($1,'tenant-a','Tenant A Org','tenant-a-org',$2)`,orgA,identityA);err!=nil{t.Fatal(err)}

 crossTenantMemberID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role) VALUES($1,$2,'tenant-a',$3,'customer','member')`,crossTenantMemberID,orgA,identityB);err==nil {
  t.Fatal("cross-tenant identity was accepted as an organization member")
 }

 ownerMembershipID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role) VALUES($1,$2,'tenant-a',$3,'customer','owner')`,ownerMembershipID,orgA,identityA);err!=nil{t.Fatal(err)}
 propertyID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO properties(id,tenant_id,organization_id,property_code,name,country_code,region_code,city_code,area_code,created_by_identity_id) VALUES($1,'tenant-a',$2,'HQ','Headquarters','OM','muscat','muscat','al-khuwair',$3)`,propertyID,orgA,identityA);err!=nil{t.Fatal(err)}

 crossTenantPropertyID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO properties(id,tenant_id,organization_id,property_code,name,country_code,region_code,city_code,area_code,created_by_identity_id) VALUES($1,'tenant-b',$2,'FOREIGN','Foreign property','OM','muscat','muscat','al-khuwair',$3)`,crossTenantPropertyID,orgA,identityB);err==nil {
  t.Fatal("cross-tenant property reference was accepted")
 }

 auditID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organization_audit_events(id,organization_id,tenant_id,actor_identity_id,action) VALUES($1,$2,'tenant-a',$3,'organization.created')`,auditID,orgA,identityA);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`UPDATE organization_audit_events SET action='tampered' WHERE id=$1`,auditID);err==nil {
  t.Fatal("organization audit event mutation was accepted")
 }
}
