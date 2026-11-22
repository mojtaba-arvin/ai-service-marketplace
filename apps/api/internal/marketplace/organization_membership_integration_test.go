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

func TestOrganizationMembershipLifecycleAndInvitationInboxPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	for _, name := range []string{"001_walking_skeleton.sql", "002_identity.sql", "022_identity_memberships.sql", "024_organizations_properties.sql"} {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = execMigrationScript(ctx, pool, raw); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	tenantID, otherTenantID := mustNewID(t), mustNewID(t)
	ownerID, inviteeID, strangerID := mustNewID(t), mustNewID(t), mustNewID(t)
	for i, entry := range []struct {
		identityID string
		tenantID   string
	}{
		{ownerID, tenantID},
		{inviteeID, tenantID},
		{strangerID, otherTenantID},
	} {
		phone := fmt.Sprintf("+180%d%010d", i, time.Now().UnixNano()%10_000_000_000)
		if _, err = pool.Exec(ctx, `INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`, entry.identityID, phone); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO identity_memberships(id,identity_id,tenant_id,role) VALUES($1,$2,$3,'customer')`, mustNewID(t), entry.identityID, entry.tenantID); err != nil {
			t.Fatal(err)
		}
	}

	store := &Store{DB: pool}
	org, err := store.CreateOrganizationForIdentity(ctx, ownerID, tenantID, CreateOrganizationInput{
		Name: "Membership Lifecycle Test",
		Slug: "membership-lifecycle-" + tenantID[:8],
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}

	if _, err = store.ChangeOrganizationMemberRole(ctx, ownerID, tenantID, org.ID, ownerID, "admin"); err != ErrMembershipConflict {
		t.Fatalf("demoting the only owner returned %v, want ErrMembershipConflict", err)
	}
	if err = store.RevokeOrganizationMember(ctx, ownerID, tenantID, org.ID, ownerID); err != ErrMembershipForbidden {
		t.Fatalf("self-revocation returned %v, want ErrMembershipForbidden", err)
	}

	invited, err := store.InviteOrganizationIdentity(ctx, ownerID, tenantID, org.ID, OrganizationMembershipInput{
		IdentityID: inviteeID,
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("invite identity: %v", err)
	}
	if invited.Status != "invited" || invited.Role != "member" {
		t.Fatalf("unexpected invitation result: %+v", invited)
	}

	inbox, err := store.ListOrganizationInvitationsForIdentity(ctx, inviteeID, tenantID)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("invitee inbox = %+v, err=%v; want exactly one invitation", inbox, err)
	}
	if inbox[0].OrganizationID != org.ID || inbox[0].MembershipID == "" || inbox[0].Role != "member" {
		t.Fatalf("inbox returned incorrect invitation: %+v", inbox[0])
	}
	if otherInbox, listErr := store.ListOrganizationInvitationsForIdentity(ctx, strangerID, tenantID); listErr != nil || len(otherInbox) != 0 {
		t.Fatalf("another identity saw invitation: %+v, err=%v", otherInbox, listErr)
	}
	if otherInbox, listErr := store.ListOrganizationInvitationsForIdentity(ctx, inviteeID, otherTenantID); listErr != nil || len(otherInbox) != 0 {
		t.Fatalf("invitation leaked across tenants: %+v, err=%v", otherInbox, listErr)
	}
	if _, err = store.AcceptOrganizationInvitation(ctx, strangerID, tenantID, org.ID); err != ErrMembershipNotFound {
		t.Fatalf("non-invitee acceptance returned %v, want ErrMembershipNotFound", err)
	}

	accepted, err := store.AcceptOrganizationInvitation(ctx, inviteeID, tenantID, org.ID)
	if err != nil || accepted.Status != "active" {
		t.Fatalf("accept invitation = %+v, err=%v", accepted, err)
	}
	inbox, err = store.ListOrganizationInvitationsForIdentity(ctx, inviteeID, tenantID)
	if err != nil || len(inbox) != 0 {
		t.Fatalf("accepted invitation remained pending: %+v, err=%v", inbox, err)
	}
	if _, err = store.InviteOrganizationIdentity(ctx, ownerID, tenantID, org.ID, OrganizationMembershipInput{
		IdentityID: inviteeID,
		Role:       "member",
	}); err != ErrMembershipConflict {
		t.Fatalf("duplicate active invitation returned %v, want ErrMembershipConflict", err)
	}

	property, err := store.CreatePropertyForIdentity(ctx, ownerID, tenantID, org.ID, CreatePropertyInput{
		Code: "HQ-" + org.ID[:8], Name: "Headquarters", CountryCode: "OM",
		RegionCode: "muscat", CityCode: "muscat", AreaCode: "al-khuwair",
	})
	if err != nil {
		t.Fatalf("create property: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO property_memberships(
			id,property_id,organization_id,tenant_id,identity_id,identity_role,role,
			created_by_identity_id,created_by_identity_role
		) VALUES($1,$2,$3,$4,$5,'customer','viewer',$6,'customer')
	`, mustNewID(t), property.ID, org.ID, tenantID, inviteeID, ownerID); err != nil {
		t.Fatal(err)
	}

	changed, err := store.ChangeOrganizationMemberRole(ctx, ownerID, tenantID, org.ID, inviteeID, "viewer")
	if err != nil || changed.Role != "viewer" {
		t.Fatalf("change member role = %+v, err=%v", changed, err)
	}
	if err = store.RevokeOrganizationMember(ctx, ownerID, tenantID, org.ID, inviteeID); err != nil {
		t.Fatalf("revoke member: %v", err)
	}

	var delegationStatus string
	if err = pool.QueryRow(ctx, `
		SELECT status FROM property_memberships
		WHERE property_id=$1 AND tenant_id=$2 AND identity_id=$3
	`, property.ID, tenantID, inviteeID).Scan(&delegationStatus); err != nil {
		t.Fatal(err)
	}
	if delegationStatus != "revoked" {
		t.Fatalf("property delegation status = %q, want revoked", delegationStatus)
	}
	if _, err = store.GetOrganizationForIdentity(ctx, inviteeID, tenantID, org.ID); err == nil {
		t.Fatal("revoked organization member retained organization access")
	}

	var auditCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM organization_audit_events WHERE organization_id=$1`, org.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 6 {
		t.Fatalf("audit event count = %d, want 6 lifecycle events", auditCount)
	}
}
