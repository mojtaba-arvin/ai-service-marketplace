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

func TestOrganizationPropertyLifecycleAndDelegationsPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
	ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil{t.Fatal(err)}
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","022_identity_memberships.sql","024_organizations_properties.sql"}{
		raw,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil{t.Fatal(readErr)}
		if _,err=execMigrationScript(ctx,pool,raw);err!=nil{t.Fatalf("apply %s: %v",name,err)}
	}

	tenantID,otherTenantID:=mustNewID(t),mustNewID(t)
	ids:=map[string]string{
		"owner":mustNewID(t),"admin":mustNewID(t),"manager":mustNewID(t),
		"member":mustNewID(t),"viewer":mustNewID(t),"outsider":mustNewID(t),
	}
	for i,identityID:=range []string{ids["owner"],ids["admin"],ids["manager"],ids["member"],ids["viewer"],ids["outsider"]}{
		tenant:=tenantID
		if identityID==ids["outsider"]{tenant=otherTenantID}
		phone:=fmt.Sprintf("+199%d%010d",i,time.Now().UnixNano()%10_000_000_000)
		if _,err=pool.Exec(ctx,"INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)",identityID,phone);err!=nil{t.Fatal(err)}
		if _,err=pool.Exec(ctx,"INSERT INTO identity_memberships(id,identity_id,tenant_id,role) VALUES($1,$2,$3,'customer')",mustNewID(t),identityID,tenant);err!=nil{t.Fatal(err)}
	}

	store:=&Store{DB:pool}
	org,err:=store.CreateOrganizationForIdentity(ctx,ids["owner"],tenantID,CreateOrganizationInput{Name:"Lifecycle Org",Slug:"lifecycle-"+tenantID[:8]})
	if err!=nil{t.Fatalf("create org: %v",err)}
	for name,role:=range map[string]string{"admin":"admin","manager":"manager","member":"member","viewer":"viewer"}{
		if _,err=pool.Exec(ctx,"INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role,status) VALUES($1,$2,$3,$4,'customer',$5,'active')",mustNewID(t),org.ID,tenantID,ids[name],role);err!=nil{t.Fatal(err)}
	}
	property,err:=store.CreatePropertyForIdentity(ctx,ids["owner"],tenantID,org.ID,CreatePropertyInput{Code:"HQ-"+org.ID[:8],Name:"Headquarters",CountryCode:"OM",RegionCode:"muscat",CityCode:"muscat",AreaCode:"al-khuwair"})
	if err!=nil{t.Fatalf("create property: %v",err)}

	updatedOrg,err:=store.UpdateOrganizationForIdentity(ctx,ids["owner"],tenantID,org.ID,UpdateOrganizationInput{Name:stringPointer("Lifecycle Org Updated")})
	if err!=nil||updatedOrg.Name!="Lifecycle Org Updated"{t.Fatalf("owner organization update: %+v err=%v",updatedOrg,err)}
	if _,err=store.UpdateOrganizationForIdentity(ctx,ids["manager"],tenantID,org.ID,UpdateOrganizationInput{Name:stringPointer("forbidden")});err!=ErrMembershipForbidden{
		t.Fatalf("manager organization update err=%v, want forbidden",err)
	}
	if _,err=store.UpdateOrganizationForIdentity(ctx,ids["owner"],otherTenantID,org.ID,UpdateOrganizationInput{Name:stringPointer("cross tenant")});err!=ErrOrganizationWriteForbidden{
		t.Fatalf("cross-tenant organization update err=%v, want denied",err)
	}
	otherOrg,err:=store.CreateOrganizationForIdentity(ctx,ids["owner"],tenantID,CreateOrganizationInput{Name:"Other Org",Slug:"other-"+tenantID[:8]})
	if err!=nil{t.Fatal(err)}
	if _,err=store.UpdateOrganizationForIdentity(ctx,ids["owner"],tenantID,org.ID,UpdateOrganizationInput{Slug:&otherOrg.Slug});!isOrganizationConflict(err){
		t.Fatalf("duplicate organization slug err=%v, want unique conflict",err)
	}

	updatedProperty,err:=store.UpdatePropertyForIdentity(ctx,ids["manager"],tenantID,org.ID,property.ID,UpdatePropertyInput{Name:stringPointer("HQ Updated"),CityCode:stringPointer("seeb")})
	if err!=nil||updatedProperty.Name!="HQ Updated"||updatedProperty.CityCode!="seeb"{t.Fatalf("manager property update: %+v err=%v",updatedProperty,err)}
	if _,err=store.UpdatePropertyForIdentity(ctx,ids["viewer"],tenantID,org.ID,property.ID,UpdatePropertyInput{Name:stringPointer("forbidden")});err!=ErrMembershipForbidden{
		t.Fatalf("viewer property update err=%v, want forbidden",err)
	}
	if _,err=store.UpdatePropertyForIdentity(ctx,ids["outsider"],otherTenantID,org.ID,property.ID,UpdatePropertyInput{Name:stringPointer("cross tenant")});err!=ErrOrganizationWriteForbidden{
		t.Fatalf("cross-tenant property update err=%v, want denied",err)
	}

	granted,err:=store.GrantPropertyDelegation(ctx,ids["owner"],tenantID,org.ID,property.ID,PropertyDelegationInput{IdentityID:ids["member"],Role:"viewer"})
	if err!=nil||granted.Status!="active"||granted.CreatedAt.IsZero(){t.Fatalf("grant viewer delegation: %+v err=%v",granted,err)}

	// Member<->viewer transitions keep an eligible explicit grant. Crossing
	// into an ineligible organization role revokes it transactionally.
	if _,err=store.ChangeOrganizationMemberRole(ctx,ids["owner"],tenantID,org.ID,ids["member"],"viewer");err!=nil{t.Fatalf("member to viewer role change: %v",err)}
	var roleChangeDelegationStatus string
	if err=pool.QueryRow(ctx,"SELECT status FROM property_memberships WHERE property_id=$1 AND identity_id=$2",property.ID,ids["member"]).Scan(&roleChangeDelegationStatus);err!=nil{t.Fatal(err)}
	if roleChangeDelegationStatus!="active"{t.Fatalf("member-to-viewer role change unexpectedly revoked delegation: %s",roleChangeDelegationStatus)}
	if _,err=store.ChangeOrganizationMemberRole(ctx,ids["owner"],tenantID,org.ID,ids["member"],"member");err!=nil{t.Fatalf("viewer to member role change: %v",err)}
	if _,err=store.ChangeOrganizationMemberRole(ctx,ids["owner"],tenantID,org.ID,ids["member"],"manager");err!=nil{t.Fatalf("member to manager role change: %v",err)}
	if err=pool.QueryRow(ctx,"SELECT status FROM property_memberships WHERE property_id=$1 AND identity_id=$2",property.ID,ids["member"]).Scan(&roleChangeDelegationStatus);err!=nil{t.Fatal(err)}
	if roleChangeDelegationStatus!="revoked"{t.Fatalf("promotion to manager left delegation active: %s",roleChangeDelegationStatus)}
	if _,err=store.ChangeOrganizationMemberRole(ctx,ids["owner"],tenantID,org.ID,ids["member"],"member");err!=nil{t.Fatalf("manager to member role change: %v",err)}
	if err=pool.QueryRow(ctx,"SELECT status FROM property_memberships WHERE property_id=$1 AND identity_id=$2",property.ID,ids["member"]).Scan(&roleChangeDelegationStatus);err!=nil{t.Fatal(err)}
	if roleChangeDelegationStatus!="revoked"{t.Fatalf("demotion resurrected stale delegation: %s",roleChangeDelegationStatus)}
	if _,err=store.GrantPropertyDelegation(ctx,ids["owner"],tenantID,org.ID,property.ID,PropertyDelegationInput{IdentityID:ids["member"],Role:"viewer"});err!=nil{t.Fatalf("explicit regrant after demotion: %v",err)}
	if _,err=store.GrantPropertyDelegation(ctx,ids["owner"],tenantID,org.ID,property.ID,PropertyDelegationInput{IdentityID:ids["outsider"],Role:"viewer"});err!=ErrMembershipNotFound{t.Fatalf("cross-tenant delegation target err=%v, want not found",err)}
	if _,err=store.GrantPropertyDelegation(ctx,ids["manager"],tenantID,org.ID,property.ID,PropertyDelegationInput{IdentityID:ids["viewer"],Role:"manager"});err!=ErrMembershipForbidden{
		t.Fatalf("manager grant manager role err=%v, want forbidden",err)
	}
	managerGrant,err:=store.GrantPropertyDelegation(ctx,ids["manager"],tenantID,org.ID,property.ID,PropertyDelegationInput{IdentityID:ids["viewer"],Role:"viewer"})
	if err!=nil{t.Fatalf("manager grant viewer: %+v err=%v",managerGrant,err)}
	if _,err=store.ChangePropertyDelegationRole(ctx,ids["manager"],tenantID,org.ID,property.ID,ids["member"],"manager");err!=ErrMembershipForbidden{
		t.Fatalf("manager change delegation role err=%v, want forbidden",err)
	}
	if err=store.RevokePropertyDelegation(ctx,ids["manager"],tenantID,org.ID,property.ID,ids["viewer"]);err!=nil{
		t.Fatalf("manager revoke viewer delegation: %v",err)
	}
	changed,err:=store.ChangePropertyDelegationRole(ctx,ids["owner"],tenantID,org.ID,property.ID,ids["member"],"manager")
	if err!=nil||changed.Role!="manager"{t.Fatalf("owner change delegation role: %+v err=%v",changed,err)}
	if err=store.RevokePropertyDelegation(ctx,ids["manager"],tenantID,org.ID,property.ID,ids["member"]);err!=ErrMembershipForbidden{t.Fatalf("manager revoke manager delegation err=%v, want forbidden",err)}
	if err=store.RevokePropertyDelegation(ctx,ids["owner"],tenantID,org.ID,property.ID,ids["member"]);err!=nil{t.Fatalf("owner revoke delegation: %v",err)}
	if _,err=store.GrantPropertyDelegation(ctx,ids["owner"],tenantID,org.ID,property.ID,PropertyDelegationInput{IdentityID:ids["viewer"],Role:"viewer"});err!=nil{t.Fatalf("owner regrant viewer delegation: %v",err)}
	if _,err=store.ListPropertyDelegationsForIdentity(ctx,ids["viewer"],tenantID,org.ID,property.ID);err!=ErrMembershipForbidden{
		t.Fatalf("ordinary member listed property delegations: %v",err)
	}
	if items,err:=store.ListPropertyDelegationsForIdentity(ctx,ids["owner"],tenantID,org.ID,property.ID);err!=nil||len(items)!=1||items[0].IdentityID!=ids["viewer"]{
		t.Fatalf("owner delegation list: %+v err=%v",items,err)
	}

	if err=store.ArchivePropertyForIdentity(ctx,ids["manager"],tenantID,org.ID,property.ID);err!=nil{t.Fatalf("archive property: %v",err)}
	var propertyStatus,delegationStatus string
	if err=pool.QueryRow(ctx,"SELECT status FROM properties WHERE id=$1",property.ID).Scan(&propertyStatus);err!=nil{t.Fatal(err)}
	if err=pool.QueryRow(ctx,"SELECT status FROM property_memberships WHERE property_id=$1 AND identity_id=$2",property.ID,ids["viewer"]).Scan(&delegationStatus);err!=nil{t.Fatal(err)}
	if propertyStatus!="archived"||delegationStatus!="revoked"{t.Fatalf("archive state property=%s delegation=%s",propertyStatus,delegationStatus)}
	if _,err=store.UpdatePropertyForIdentity(ctx,ids["owner"],tenantID,org.ID,property.ID,UpdatePropertyInput{Name:stringPointer("after archive")});err!=ErrMembershipNotFound{
		t.Fatalf("archived property update err=%v, want not found",err)
	}

	orgToArchive,err:=store.CreateOrganizationForIdentity(ctx,ids["owner"],tenantID,CreateOrganizationInput{Name:"Archive Org",Slug:"archive-"+tenantID[:8]})
	if err!=nil{t.Fatal(err)}
	if _,err=pool.Exec(ctx,"INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role,status) VALUES($1,$2,$3,$4,'customer','admin','active'),($5,$2,$3,$6,'customer','viewer','active')",mustNewID(t),orgToArchive.ID,tenantID,ids["admin"],mustNewID(t),ids["viewer"]);err!=nil{t.Fatal(err)}
	archiveProperty,err:=store.CreatePropertyForIdentity(ctx,ids["owner"],tenantID,orgToArchive.ID,CreatePropertyInput{Code:"AR-"+orgToArchive.ID[:8],Name:"Archive Property",CountryCode:"OM",RegionCode:"muscat",CityCode:"muscat",AreaCode:"ruwi"})
	if err!=nil{t.Fatal(err)}
	if _,err=store.GrantPropertyDelegation(ctx,ids["owner"],tenantID,orgToArchive.ID,archiveProperty.ID,PropertyDelegationInput{IdentityID:ids["viewer"],Role:"viewer"});err!=nil{t.Fatal(err)}
	if err=store.ArchiveOrganizationForIdentity(ctx,ids["admin"],tenantID,orgToArchive.ID);err!=ErrMembershipForbidden{
		t.Fatalf("admin organization archive err=%v, want forbidden",err)
	}
	if err=store.ArchiveOrganizationForIdentity(ctx,ids["owner"],tenantID,orgToArchive.ID);err!=nil{t.Fatalf("owner archive organization: %v",err)}
	var orgStatus,archivedPropertyStatus,revokedMembershipStatus string
	if err=pool.QueryRow(ctx,"SELECT status FROM organizations WHERE id=$1",orgToArchive.ID).Scan(&orgStatus);err!=nil{t.Fatal(err)}
	if err=pool.QueryRow(ctx,"SELECT status FROM properties WHERE id=$1",archiveProperty.ID).Scan(&archivedPropertyStatus);err!=nil{t.Fatal(err)}
	if err=pool.QueryRow(ctx,"SELECT status FROM property_memberships WHERE property_id=$1 AND identity_id=$2",archiveProperty.ID,ids["viewer"]).Scan(&revokedMembershipStatus);err!=nil{t.Fatal(err)}
	var activeMemberships int
	if err=pool.QueryRow(ctx,"SELECT count(*) FROM organization_memberships WHERE organization_id=$1 AND status='active'",orgToArchive.ID).Scan(&activeMemberships);err!=nil{t.Fatal(err)}
	if orgStatus!="archived"||archivedPropertyStatus!="archived"||revokedMembershipStatus!="revoked"||activeMemberships!=0{
		t.Fatalf("organization archive did not preserve records and revoke access: org=%s property=%s delegation=%s active_memberships=%d",orgStatus,archivedPropertyStatus,revokedMembershipStatus,activeMemberships)
	}
	if _,err=store.GetOrganizationForIdentity(ctx,ids["owner"],tenantID,orgToArchive.ID);err==nil{
		t.Fatal("archived organization remained readable")
	}
}

func TestOrganizationPropertyPatchValidation(t *testing.T) {
	if err:=validateOrganizationPatch(UpdateOrganizationInput{});err!=ErrOrganizationInput{t.Fatalf("empty organization patch err=%v",err)}
	if err:=validateOrganizationPatch(UpdateOrganizationInput{Name:stringPointer(" ")});err!=ErrOrganizationInput{t.Fatalf("blank organization name err=%v",err)}
	if err:=validateOrganizationPatch(UpdateOrganizationInput{Slug:stringPointer("Invalid Slug")});err!=ErrOrganizationInput{t.Fatalf("invalid slug err=%v",err)}
	if _,err:=normalizePropertyPatch(UpdatePropertyInput{});err!=ErrOrganizationInput{t.Fatalf("empty property patch err=%v",err)}
	if _,err:=normalizePropertyPatch(UpdatePropertyInput{CountryCode:stringPointer("USA")});err!=ErrOrganizationInput{t.Fatalf("invalid country code err=%v",err)}
	if _,err:=normalizePropertyPatch(UpdatePropertyInput{Name:stringPointer("  New Name  ")});err!=nil{t.Fatalf("valid property patch err=%v",err)}
}

func stringPointer(value string)*string{return &value}

func TestIsOrganizationRootPath(t *testing.T) {
	cases:=[]struct{path string;want bool}{
		{"/v1/organizations/org-1",true},
		{"/v1/organizations/org-1/",false},
		{"/v1/organizations/org-1/memberships/member-1/role",false},
		{"/v1/organizations/org-1/properties/property-1",false},
		{"/v1/organizations/",false},
		{"/v1/other/org-1",false},
	}
	for _,tc:=range cases {
		if got:=isOrganizationRootPath(tc.path);got!=tc.want {
			t.Errorf("isOrganizationRootPath(%q)=%v want %v",tc.path,got,tc.want)
		}
	}
}
