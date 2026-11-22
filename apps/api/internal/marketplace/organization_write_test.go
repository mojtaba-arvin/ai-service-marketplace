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

func TestOrganizationAndPropertyWritesAreAtomicAndAuditedPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()

	for _, name := range []string{"001_walking_skeleton.sql", "002_identity.sql", "022_identity_memberships.sql", "024_organizations_properties.sql"} {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil { t.Fatal(readErr) }
		if _, err = execMigrationScript(ctx, pool, raw); err != nil { t.Fatalf("apply %s: %v", name, err) }
	}

	tenantID, ownerID, viewerID := mustNewID(t), mustNewID(t), mustNewID(t)
	for i, identityID := range []string{ownerID, viewerID} {
		phone := fmt.Sprintf("+197%d%010d", i, time.Now().UnixNano()%10_000_000_000)
		if _, err = pool.Exec(ctx, `INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`, identityID, phone); err != nil { t.Fatal(err) }
		if _, err = pool.Exec(ctx, `INSERT INTO identity_memberships(id,identity_id,tenant_id,role) VALUES($1,$2,$3,'customer')`, mustNewID(t), identityID, tenantID); err != nil { t.Fatal(err) }
	}

	store := &Store{DB: pool}
	org, err := store.CreateOrganizationForIdentity(ctx, ownerID, tenantID, CreateOrganizationInput{Name:"Write Test Org", Slug:"write-test-" + tenantID[:8]})
	if err != nil { t.Fatalf("create organization: %v", err) }
	if org.Role != "owner" || org.Status != "active" { t.Fatalf("unexpected organization result: %+v", org) }

	property, err := store.CreatePropertyForIdentity(ctx, ownerID, tenantID, org.ID, CreatePropertyInput{
		Code:"HQ-" + org.ID[:8], Name:"Headquarters", CountryCode:"om",
		RegionCode:"muscat", CityCode:"muscat", AreaCode:"al-khuwair",
	})
	if err != nil { t.Fatalf("create property: %v", err) }
	if property.Status != "active" || property.AccessRole != "owner" || property.CountryCode != "OM" {
		t.Fatalf("unexpected property result: %+v", property)
	}

	var auditCount, outboxCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM organization_audit_events WHERE organization_id=$1`, org.ID).Scan(&auditCount); err != nil { t.Fatal(err) }
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id IN ($1,$2) AND event_type IN ('organization.created','property.created')`, org.ID, property.ID).Scan(&outboxCount); err != nil { t.Fatal(err) }
	if auditCount != 2 || outboxCount != 2 { t.Fatalf("expected 2 audit and 2 outbox rows, got audit=%d outbox=%d", auditCount, outboxCount) }

	if _, err = store.CreatePropertyForIdentity(ctx, viewerID, tenantID, org.ID, CreatePropertyInput{
		Code:"NOPE", Name:"Unauthorized", CountryCode:"OM", RegionCode:"muscat", CityCode:"muscat", AreaCode:"al-khuwair",
	}); err != ErrOrganizationWriteForbidden {
		t.Fatalf("viewer property creation error = %v, want ErrOrganizationWriteForbidden", err)
	}
	if _, err = store.CreateOrganizationForIdentity(ctx, viewerID, "missing-tenant", CreateOrganizationInput{Name:"No Membership", Slug:"no-membership"}); err != ErrOrganizationWriteForbidden {
		t.Fatalf("cross-tenant organization creation error = %v, want ErrOrganizationWriteForbidden", err)
	}

	var propertyCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM properties WHERE organization_id=$1`, org.ID).Scan(&propertyCount); err != nil { t.Fatal(err) }
	if propertyCount != 1 { t.Fatalf("unauthorized write changed property count: %d", propertyCount) }
}

func TestOrganizationWriteValidation(t *testing.T) {
	if validateCreateOrganization(CreateOrganizationInput{Name:" Org ",Slug:"valid-org"}) != nil { t.Fatal("valid organization rejected") }
	if validateCreateOrganization(CreateOrganizationInput{Name:"Org",Slug:"Upper-Case"}) == nil { t.Fatal("uppercase slug accepted") }
	if validateCreateProperty(CreatePropertyInput{Code:"HQ",Name:"Office",CountryCode:"OM",RegionCode:"r",CityCode:"c",AreaCode:"a"}) != nil { t.Fatal("valid property rejected") }
	if validateCreateProperty(CreatePropertyInput{Code:"HQ",Name:"Office",CountryCode:"O1",RegionCode:"r",CityCode:"c",AreaCode:"a"}) == nil { t.Fatal("invalid country code accepted") }
}
