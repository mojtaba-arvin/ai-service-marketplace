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

func TestOrganizationManagementLifecyclePostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","022_identity_memberships.sql","024_organizations_properties.sql"} {
		raw,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx,pool,raw);err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	tenantID,ownerID,viewerID:=mustNewID(t),mustNewID(t),mustNewID(t)
	for i,id:=range []string{ownerID,viewerID} {
		phone:=fmt.Sprintf("+197%d%010d",i,time.Now().UnixNano()%10_000_000_000)
		if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,id,phone);err!=nil { t.Fatal(err) }
		if _,err=pool.Exec(ctx,`INSERT INTO identity_memberships(id,identity_id,tenant_id,role) VALUES($1,$2,$3,'customer')`,mustNewID(t),id,tenantID);err!=nil { t.Fatal(err) }
	}
	store:=&Store{DB:pool}
	org,err:=store.CreateOrganizationForIdentity(ctx,ownerID,tenantID,"Lifecycle Test","lifecycle-"+ownerID[:8])
	if err!=nil { t.Fatalf("create organization: %v",err) }
	if org.Role!="owner"||org.Status!="active" { t.Fatalf("unexpected owner result: %+v",org) }
	property,err:=store.CreatePropertyForIdentity(ctx,ownerID,tenantID,org.ID,PropertyCreateInput{
		Code:"HQ-"+ownerID[:6],Name:"Headquarters",CountryCode:"OM",RegionCode:"muscat",CityCode:"muscat",AreaCode:"al-khuwair",
	})
	if err!=nil { t.Fatalf("create property: %v",err) }
	if property.Status!="active"||property.AccessRole!="owner" { t.Fatalf("unexpected property result: %+v",property) }
	invitation,err:=store.InviteOrganizationMember(ctx,ownerID,tenantID,org.ID,OrganizationInvitationInput{
		IdentityID:viewerID,IdentityRole:"customer",Role:"viewer",
	})
	if err!=nil { t.Fatalf("invite member: %v",err) }
	if invitation.Status!="invited" { t.Fatalf("invitation was not pending: %+v",invitation) }
	pending,err:=store.ListPendingOrganizationInvitations(ctx,viewerID,tenantID)
	if err!=nil||len(pending)!=1||pending[0].ID!=invitation.ID { t.Fatalf("pending invitation not visible to invitee: %+v %v",pending,err) }
	if err=store.AcceptOrganizationInvitation(ctx,viewerID,tenantID,invitation.ID);err!=nil { t.Fatalf("accept invitation: %v",err) }
	pending,err=store.ListPendingOrganizationInvitations(ctx,viewerID,tenantID)
	if err!=nil||len(pending)!=0 { t.Fatalf("accepted invitation remained pending: %+v %v",pending,err) }
	if _,err=store.CreatePropertyForIdentity(ctx,viewerID,tenantID,org.ID,PropertyCreateInput{
		Code:"NOPE",Name:"Unauthorized",CountryCode:"OM",RegionCode:"muscat",CityCode:"muscat",AreaCode:"al-khuwair",
	});!errors.Is(err,ErrOrganizationForbidden) { t.Fatalf("viewer created a property: %v",err) }
	if err=store.ChangeOrganizationMemberRole(ctx,ownerID,tenantID,org.ID,invitation.ID,"member");err!=nil { t.Fatalf("change member role: %v",err) }
	if err=store.RevokeOrganizationMember(ctx,ownerID,tenantID,org.ID,invitation.ID);err!=nil { t.Fatalf("revoke member: %v",err) }
	if _,err=store.GetOrganizationForIdentity(ctx,viewerID,tenantID,org.ID);!errors.Is(err,ErrOrganizationAccessDenied) { t.Fatalf("revoked member retained organization access: %v",err) }
	var audits,events int
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM organization_audit_events WHERE organization_id=$1 AND tenant_id=$2`,org.ID,tenantID).Scan(&audits);err!=nil { t.Fatal(err) }
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM outbox_events WHERE aggregate_type='organization' AND aggregate_id=$1`,org.ID).Scan(&events);err!=nil { t.Fatal(err) }
	if audits!=6||events!=6 { t.Fatalf("expected six atomic audit/events; got audits=%d events=%d",audits,events) }
}

func TestOrganizationManagementPathParsing(t *testing.T) {
	cases:=[]struct{path,orgID,resourceID,action string;ok bool}{
		{"/v1/organizations/org-1","org-1","","",true},
		{"/v1/organizations/org-1/properties","org-1","","properties",true},
		{"/v1/organizations/org-1/invitations","org-1","","invitations",true},
		{"/v1/organizations/org-1/members/member-1/accept","org-1","member-1","accept",true},
		{"/v1/organizations/org-1/members/member-1/revoke","org-1","member-1","revoke",true},
		{"/v1/organizations/org-1/members/member-1/role","org-1","member-1","role",true},
		{"/v1/organizations/org-1/members/member-1/unknown","","","",false},
	}
	for _,tc:=range cases {
		orgID,resourceID,action,ok:=parseOrganizationManagementPath(tc.path)
		if orgID!=tc.orgID||resourceID!=tc.resourceID||action!=tc.action||ok!=tc.ok { t.Errorf("parseOrganizationManagementPath(%q)=(%q,%q,%q,%v)",tc.path,orgID,resourceID,action,ok) }
	}
}
