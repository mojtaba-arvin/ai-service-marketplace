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

func TestOrganizationReadsRequireActiveMembershipAndPropertyDelegationPostgres(t *testing.T) {
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

	tenantID,err:=newID();if err!=nil{t.Fatal(err)}
	ownerID,err:=newID();if err!=nil{t.Fatal(err)}
	viewerID,err:=newID();if err!=nil{t.Fatal(err)}
	for i,id:=range []string{ownerID,viewerID} {
		phone:=fmt.Sprintf("+198%d%010d",i,time.Now().UnixNano()%10_000_000_000)
		if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,id,phone);err!=nil{t.Fatal(err)}
		if _,err=pool.Exec(ctx,`INSERT INTO identity_memberships(id,identity_id,tenant_id,role) VALUES($1,$2,$3,'customer')`,mustNewID(t),id,tenantID);err!=nil{t.Fatal(err)}
	}

	orgID,err:=newID();if err!=nil{t.Fatal(err)}
	if _,err=pool.Exec(ctx,`INSERT INTO organizations(id,tenant_id,name,slug,created_by_identity_id,created_by_identity_role) VALUES($1,$2,'Access Test Org',$3,$4,'customer')`,orgID,tenantID,"access-"+orgID[:8],ownerID);err!=nil{t.Fatal(err)}
	ownerMembershipID,err:=newID();if err!=nil{t.Fatal(err)}
	viewerMembershipID,err:=newID();if err!=nil{t.Fatal(err)}
	if _,err=pool.Exec(ctx,`
		INSERT INTO organization_memberships(id,organization_id,tenant_id,identity_id,identity_role,role,status)
		VALUES($1,$2,$3,$4,'customer','owner','active'),($5,$2,$3,$6,'customer','viewer','active')
	`,ownerMembershipID,orgID,tenantID,ownerID,viewerMembershipID,viewerID);err!=nil{t.Fatal(err)}

	propertyIDs:=make([]string,0,2)
	for i:=0;i<2;i++ {
		id,e:=newID();if e!=nil{t.Fatal(e)}
		propertyIDs=append(propertyIDs,id)
		if _,err=pool.Exec(ctx,`
			INSERT INTO properties(id,tenant_id,organization_id,property_code,name,country_code,region_code,city_code,area_code,created_by_identity_id,created_by_identity_role)
			VALUES($1,$2,$3,$4,$5,'OM','muscat','muscat','al-khuwair',$6,'customer')
		`,id,tenantID,orgID,fmt.Sprintf("P-%d-%s",i,id[:6]),fmt.Sprintf("Property %d",i),ownerID);err!=nil{t.Fatal(err)}
	}
	if _,err=pool.Exec(ctx,`
		INSERT INTO property_memberships(id,property_id,organization_id,tenant_id,identity_id,identity_role,role,created_by_identity_id,created_by_identity_role)
		VALUES($1,$2,$3,$4,$5,'customer','viewer',$6,'customer')
	`,mustNewID(t),propertyIDs[0],orgID,tenantID,viewerID,ownerID);err!=nil{t.Fatal(err)}

	store:=&Store{DB:pool}
	organizations,err:=store.ListOrganizationsForIdentity(ctx,viewerID,tenantID)
	if err!=nil||len(organizations)!=1||organizations[0].Role!="viewer" { t.Fatalf("viewer organization visibility: items=%+v err=%v",organizations,err) }
	ownerProperties,err:=store.ListPropertiesForIdentity(ctx,ownerID,tenantID,orgID)
	if err!=nil||len(ownerProperties)!=2 { t.Fatalf("owner should see both properties: items=%+v err=%v",ownerProperties,err) }
	viewerProperties,err:=store.ListPropertiesForIdentity(ctx,viewerID,tenantID,orgID)
	if err!=nil||len(viewerProperties)!=1||viewerProperties[0].ID!=propertyIDs[0] { t.Fatalf("viewer should see only delegated property: items=%+v err=%v",viewerProperties,err) }
	otherTenant,err:=store.GetOrganizationForIdentity(ctx,viewerID,"different-tenant",orgID)
	if err==nil||otherTenant.ID!="" { t.Fatalf("cross-tenant organization read was allowed: %+v %v",otherTenant,err) }

	if _,err=pool.Exec(ctx,`UPDATE organization_memberships SET status='revoked',revoked_at=now(),updated_at=now() WHERE id=$1`,viewerMembershipID);err!=nil{t.Fatal(err)}
	organizations,err=store.ListOrganizationsForIdentity(ctx,viewerID,tenantID)
	if err!=nil||len(organizations)!=0 { t.Fatalf("revoked membership remained visible: items=%+v err=%v",organizations,err) }
	if _,err=store.ListPropertiesForIdentity(ctx,viewerID,tenantID,orgID);err==nil { t.Fatal("revoked member retained property access") }
}

func TestParseOrganizationPath(t *testing.T) {
	cases:=[]struct{path,id string;ok bool}{
		{"/v1/organizations","",false},
		{"/v1/organizations/org-1","org-1",true},
		{"/v1/organizations/org-1/properties","org-1",true},
		{"/v1/organizations/org-1/members","",false},
		{"/v1/organizations//properties","",false},
		{"/v1/organizations/org-1/properties/extra","",false},
	}
	for _,tc:=range cases {
		id,ok:=parseOrganizationPath(tc.path)
		if id!=tc.id||ok!=tc.ok { t.Errorf("parseOrganizationPath(%q)=(%q,%v), want (%q,%v)",tc.path,id,ok,tc.id,tc.ok) }
	}
}
