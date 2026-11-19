package marketplace

import (
	"context"
	"errors"\n\t"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServiceCategoryHierarchyPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "004_catalog.sql"))
	if err != nil { t.Fatal(err) }
	if _, err = pool.Exec(ctx, string(migration)); err != nil { t.Fatalf("catalog migration: %v", err) }
	store := &Store{DB: pool}

	rootSlug := fmt.Sprintf("home-services-%d", time.Now().UnixNano())\n\troot, err := store.CreateCategory(ctx, nil, rootSlug, "Home services")
	if err != nil { t.Fatalf("create root: %v", err) }
	parentID := root.ID
	repair, err := store.CreateCategory(ctx, &parentID, "plumbing", "Plumbing")
	if err != nil { t.Fatalf("create child: %v", err) }
	childParent := repair.ID
	leakRepair, err := store.CreateCategory(ctx, &childParent, "leak-repair", "Leak repair")
	if err != nil { t.Fatalf("create grandchild: %v", err) }

	allowed, err := store.CanAcceptOrders(ctx, leakRepair.ID)
	if err != nil || !allowed { t.Fatalf("active hierarchy should accept orders: allowed=%v err=%v", allowed, err) }
	if err = store.MoveCategory(ctx, root.ID, &childParent); !errors.Is(err, ErrInvalidCategory) {
		t.Fatalf("cycle must be rejected: %v", err)
	}
	if err = store.SetCategoryEnabled(ctx, root.ID, false); err != nil { t.Fatal(err) }
	allowed, err = store.CanAcceptOrders(ctx, leakRepair.ID)
	if err != nil || allowed { t.Fatalf("disabled ancestor must block orders: allowed=%v err=%v", allowed, err) }
	if _, err = store.CreateCategory(ctx, &parentID, "pipe-installation", "Pipe installation"); !errors.Is(err, ErrCategoryDisabled) {
		t.Fatalf("cannot add a child beneath disabled category: %v", err)
	}
	if err = store.SetCategoryEnabled(ctx, repair.ID, true); !errors.Is(err, ErrCategoryDisabled) {
		t.Fatalf("cannot enable child beneath disabled ancestor: %v", err)
	}
	if err = store.SetCategoryEnabled(ctx, root.ID, true); err != nil { t.Fatal(err) }
	if err = store.SetCategoryEnabled(ctx, repair.ID, true); err != nil { t.Fatal(err) }
	allowed, err = store.CanAcceptOrders(ctx, leakRepair.ID)
	if err != nil || !allowed { t.Fatalf("reenabled hierarchy should accept orders: allowed=%v err=%v", allowed, err) }

	duplicate, err := store.CreateCategory(ctx, nil, rootSlug, "Duplicate")
	if err == nil { t.Fatalf("duplicate root slug should fail: %#v", duplicate) }
	if _, err = store.GetCategory(ctx, "missing-category"); !errors.Is(err, ErrCategoryNotFound) {
		t.Fatalf("missing category should return typed error: %v", err)
	}
}
