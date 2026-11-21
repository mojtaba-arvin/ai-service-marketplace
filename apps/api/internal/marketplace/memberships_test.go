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

func TestIdentityMembershipResolutionPostgres(t *testing.T) {
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" {
        t.Skip("TEST_DATABASE_URL is not configured")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil {
        t.Fatal(err)
    }
    defer pool.Close()

    for _, name := range []string{"001_walking_skeleton.sql", "002_identity.sql", "022_identity_memberships.sql"} {
        raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
        if readErr != nil {
            t.Fatal(readErr)
        }
        if _, err = execMigrationScript(ctx, pool, raw); err != nil {
            t.Fatalf("apply %s: %v", name, err)
        }
    }

    identityID, err := newID()
    if err != nil {
        t.Fatal(err)
    }
    phone := fmt.Sprintf("+1995%011d", time.Now().UnixNano()%100_000_000_000)
    if _, err = pool.Exec(ctx, `INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`, identityID, phone); err != nil {
        t.Fatal(err)
    }
    store := &Store{DB: pool}

    if _, err = store.ResolveActor(ctx, identityID, "market-om", RoleOperator); !errors.Is(err, ErrMembershipNotFound) {
        t.Fatalf("unassigned operator role must be denied, got %v", err)
    }
    if err = store.EnsureMembershipForPhone(ctx, phone, "market-om", RoleCustomer); err != nil {
        t.Fatal(err)
    }
    if err = store.EnsureMembershipForPhone(ctx, phone, "market-om", RoleCustomer); err != nil {
        t.Fatalf("idempotent membership grant failed: %v", err)
    }
    customer, err := store.ResolveActor(ctx, identityID, "market-om", RoleCustomer)
    if err != nil || customer.Role != RoleCustomer || customer.TenantID != "market-om" {
        t.Fatalf("customer membership did not resolve: %#v %v", customer, err)
    }
    if _, err = store.ResolveActor(ctx, identityID, "another-market", RoleCustomer); !errors.Is(err, ErrMembershipNotFound) {
        t.Fatalf("membership must not cross tenants, got %v", err)
    }
    if err = store.EnsureIdentityMembership(ctx, identityID, "market-om", RoleProfessional); err != nil {
        t.Fatal(err)
    }
    roles, err := store.ListIdentityRoles(ctx, identityID, "market-om")
    if err != nil || len(roles) != 2 || roles[0] != RoleCustomer || roles[1] != RoleProfessional {
        t.Fatalf("unexpected role set: %#v %v", roles, err)
    }
}
