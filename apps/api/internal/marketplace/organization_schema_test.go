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

func mustNewID(t *testing.T) string { t.Helper(); id,err:=newID(); if err!=nil { t.Fatal(err) }; return id }

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
 orgSlug:="tenant-a-org-"+orgA[:8]
 if _,err=pool.Exec(ctx,`INSERT INTO organizations(id,tenant_id,name,slug,created_by_identity_id,created_by_identity_role) VALUES($1,'tenant-a','Tenant A Org',$3,$2,'customer')`,orgA,identityA,orgSlug);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organizations(id,tenant_id,name,slug,created_by_identity_id,created_by_identity_role) VALUES($1,'tenant-a','Forged creator',$3,$2,'customer')`,mustNewID(t),identityB,"forged-"+orgA[:8]);err==nil {
  t.Fatal("cross-tenant organization creator was accepted")
 }

 crossTenantMemberID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role) VALUES($1,$2,'tenant-a',$3,'customer','member')`,crossTenantMemberID,orgA,identityB);err==nil {
  t.Fatal("cross-tenant identity was accepted as an organization member")
 }

 invitedMembershipID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role,invited_by_identity_id,invited_by_identity_role) VALUES($1,$2,'tenant-a',$3,'customer','member',$4,'customer')`,invitedMembershipID,orgA,identityA,identityB);err==nil {
  t.Fatal("cross-tenant organization inviter was accepted")
 }
 ownerMembershipID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role) VALUES($1,$2,'tenant-a',$3,'customer','owner')`,ownerMembershipID,orgA,identityA);err!=nil{t.Fatal(err)}
 forgedPropertyID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO properties(id,tenant_id,organization_id,property_code,name,country_code,region_code,city_code,area_code,created_by_identity_id,created_by_identity_role) VALUES($1,'tenant-a',$2,'FORGED','Forged creator','OM','muscat','muscat','al-khuwair',$3,'customer')`,forgedPropertyID,orgA,identityB);err==nil {
  t.Fatal("cross-tenant property creator was accepted")
 }
 propertyID,err:=newID();if err!=nil{t.Fatal(err)}
 propertyCode:="HQ-"+propertyID[:8]
 if _,err=pool.Exec(ctx,`INSERT INTO properties(id,tenant_id,organization_id,property_code,name,country_code,region_code,city_code,area_code,created_by_identity_id,created_by_identity_role) VALUES($1,'tenant-a',$2,$3,'Headquarters','OM','muscat','muscat','al-khuwair',$4,'customer')`,propertyID,orgA,propertyCode,identityA);err!=nil{t.Fatal(err)}

 crossTenantDelegationID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO property_memberships(id,property_id,organization_id,tenant_id,identity_id,identity_role,role,created_by_identity_id,created_by_identity_role) VALUES($1,$2,$3,'tenant-a',$4,'customer','viewer',$5,'customer')`,crossTenantDelegationID,propertyID,orgA,identityB,identityA);err==nil {
  t.Fatal("cross-tenant identity was accepted as a property delegate")
 }
 forgedDelegationID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO property_memberships(id,property_id,organization_id,tenant_id,identity_id,identity_role,role,created_by_identity_id,created_by_identity_role) VALUES($1,$2,$3,'tenant-a',$4,'customer','viewer',$5,'customer')`,forgedDelegationID,propertyID,orgA,identityA,identityB);err==nil {
  t.Fatal("cross-tenant property delegation creator was accepted")
 }
 delegationID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO property_memberships(id,property_id,organization_id,tenant_id,identity_id,identity_role,role,created_by_identity_id,created_by_identity_role) VALUES($1,$2,$3,'tenant-a',$4,'customer','viewer',$5,'customer')`,delegationID,propertyID,orgA,identityA,identityA);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`UPDATE property_memberships SET status='revoked',revoked_at=now() WHERE id=$1`,delegationID);err!=nil{t.Fatal(err)}

 crossTenantPropertyID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO properties(id,tenant_id,organization_id,property_code,name,country_code,region_code,city_code,area_code,created_by_identity_id,created_by_identity_role) VALUES($1,'tenant-b',$2,'FOREIGN','Foreign property','OM','muscat','muscat','al-khuwair',$3,'customer')`,crossTenantPropertyID,orgA,identityB);err==nil {
  t.Fatal("cross-tenant property reference was accepted")
 }

 forgedAuditID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organization_audit_events(id,organization_id,tenant_id,actor_identity_id,actor_identity_role,action) VALUES($1,$2,'tenant-a',$3,'customer','forged.action')`,forgedAuditID,orgA,identityB);err==nil {
  t.Fatal("cross-tenant audit actor was accepted")
 }
 auditID,err:=newID();if err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`INSERT INTO organization_audit_events(id,organization_id,tenant_id,actor_identity_id,actor_identity_role,action) VALUES($1,$2,'tenant-a',$3,'customer','organization.created')`,auditID,orgA,identityA);err!=nil{t.Fatal(err)}
 if _,err=pool.Exec(ctx,`UPDATE organization_audit_events SET action='tampered' WHERE id=$1`,auditID);err==nil {
  t.Fatal("organization audit event mutation was accepted")
 }
}
