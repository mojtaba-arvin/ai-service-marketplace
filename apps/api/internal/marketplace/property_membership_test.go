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

func TestPropertyMembershipRolePolicy(t *testing.T){
 if !validPropertyMembershipRole("manager")||!validPropertyMembershipRole("viewer"){t.Fatal("supported property roles rejected")}
 for _,role:=range []string{"owner","admin","member","support",""}{if validPropertyMembershipRole(role){t.Fatalf("invalid property role accepted: %q",role)}}
 if !canManagePropertyMemberships("owner")||!canManagePropertyMemberships("admin")||!canManagePropertyMemberships("manager"){t.Fatal("organization managers must manage property delegations")}
 for _,role:=range []string{"member","viewer","operator","support",""}{if canManagePropertyMemberships(role){t.Fatalf("non-manager role can manage property delegations: %q",role)}}
}

func TestPropertyMembershipLifecyclePostgres(t *testing.T){
 dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
 for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","022_identity_memberships.sql","024_organizations_properties.sql"}{
  raw,e:=os.ReadFile(filepath.Join("..","..","migrations",name));if e!=nil{t.Fatal(e)}
  if _,err=execMigrationScript(ctx,pool,raw);err!=nil{t.Fatalf("apply %s: %v",name,err)}
 }
 tenantID,ownerID,managerID,memberID:=mustNewID(t),mustNewID(t),mustNewID(t),mustNewID(t)
 for i,id:=range []string{ownerID,managerID,memberID}{
  phone:=fmt.Sprintf("+196%d%010d",i,time.Now().UnixNano()%10_000_000_000)
  if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,id,phone);err!=nil{t.Fatal(err)}
  if _,err=pool.Exec(ctx,`INSERT INTO identity_memberships(id,identity_id,tenant_id,role) VALUES($1,$2,$3,'customer')`,mustNewID(t),id,tenantID);err!=nil{t.Fatal(err)}
 }
 store:=&Store{DB:pool}
 org,err:=store.CreateOrganizationForIdentity(ctx,ownerID,tenantID,CreateOrganizationInput{Name:"Property Access Test",Slug:"property-access-"+tenantID[:8]});if err!=nil{t.Fatal(err)}
 for _,entry:=range []struct{id,role string}{{managerID,"manager"},{memberID,"member"}}{
  if _,err=pool.Exec(ctx,`INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role,status) VALUES($1,$2,$3,$4,'customer',$5,'active')`,mustNewID(t),org.ID,tenantID,entry.id,entry.role);err!=nil{t.Fatal(err)}
 }
 property,err:=store.CreatePropertyForIdentity(ctx,ownerID,tenantID,org.ID,CreatePropertyInput{Code:"P-"+org.ID[:8],Name:"Office",CountryCode:"OM",RegionCode:"muscat",CityCode:"muscat",AreaCode:"al-khuwair"});if err!=nil{t.Fatal(err)}
 assigned,err:=store.AssignPropertyMembership(ctx,managerID,tenantID,org.ID,property.ID,PropertyMembershipInput{IdentityID:memberID,Role:"viewer"})
 if err!=nil||assigned.Status!="active"||assigned.Role!="viewer"{t.Fatalf("assign result=%+v err=%v",assigned,err)}
 if _,err=store.AssignPropertyMembership(ctx,managerID,tenantID,org.ID,property.ID,PropertyMembershipInput{IdentityID:memberID,Role:"viewer"});err!=ErrPropertyMembershipConflict{t.Fatalf("duplicate assignment error=%v",err)}
 list,err:=store.ListPropertyMemberships(ctx,managerID,tenantID,org.ID,property.ID)
 if err!=nil||len(list)!=1||list[0].IdentityID!=memberID{t.Fatalf("delegation list=%+v err=%v",list,err)}
 visible,err:=store.ListPropertiesForIdentity(ctx,memberID,tenantID,org.ID)
 if err!=nil||len(visible)!=1||visible[0].ID!=property.ID{t.Fatalf("delegated property visibility=%+v err=%v",visible,err)}
 changed,err:=store.ChangePropertyMembershipRole(ctx,managerID,tenantID,org.ID,property.ID,memberID,"manager")
 if err!=nil||changed.Role!="manager"{t.Fatalf("role change=%+v err=%v",changed,err)}
 if err=store.RevokePropertyMembership(ctx,managerID,tenantID,org.ID,property.ID,memberID);err!=nil{t.Fatal(err)}
 visible,err=store.ListPropertiesForIdentity(ctx,memberID,tenantID,org.ID)
 if err!=nil||len(visible)!=0{t.Fatalf("revoked property remained visible: %+v err=%v",visible,err)}
 if _,err=store.AssignPropertyMembership(ctx,memberID,tenantID,org.ID,property.ID,PropertyMembershipInput{IdentityID:ownerID,Role:"viewer"});err!=ErrPropertyMembershipForbidden{t.Fatalf("member grant error=%v",err)}
 if _,err=store.AssignPropertyMembership(ctx,managerID,tenantID,org.ID,property.ID,PropertyMembershipInput{IdentityID:ownerID,Role:"viewer"});err!=ErrPropertyMembershipForbidden{t.Fatalf("organization owner delegation should be unnecessary, error=%v",err)}
 var audits,events int
 if err=pool.QueryRow(ctx,`SELECT count(*) FROM organization_audit_events WHERE organization_id=$1 AND action LIKE 'property.member.%'`,org.ID).Scan(&audits);err!=nil{t.Fatal(err)}
 if err=pool.QueryRow(ctx,`SELECT count(*) FROM outbox_events WHERE aggregate_type='property_membership' AND event_type LIKE 'property.member.%' AND payload->'data'->>'property_id'=$1`,property.ID).Scan(&events);err!=nil{t.Fatal(err)}
 if audits!=3||events!=3{t.Fatalf("expected 3 atomic audit/outbox records, got audits=%d events=%d",audits,events)}
}
