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

func TestVersionedPriceMenuAndOrderSnapshotPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	for _, name := range []string{"001_walking_skeleton.sql", "004_catalog.sql", "005_pricing.sql","015_order_schedule.sql"} {
		migration, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil { t.Fatal(readErr) }
		if _, err = pool.Exec(ctx, string(migration)); err != nil { t.Fatalf("apply %s: %v", name, err) }
	}
	store := &Store{DB: pool}
	categorySlug := fmt.Sprintf("electrical-%d", time.Now().UnixNano())
	category, err := store.CreateCategory(ctx, nil, categorySlug, "Electrical")
	if err != nil { t.Fatal(err) }
	menu, err := store.CreatePriceMenu(ctx, category.ID, "socket-installation")
	customerID := fmt.Sprintf("customer-price-test-%d", time.Now().UnixNano())
	if err != nil { t.Fatal(err) }
	first, err := store.CreatePriceVersion(ctx, menu.ID, 2500, "OMR")
	if err != nil { t.Fatal(err) }
	if first.VersionNo != 1 { t.Fatalf("first version number = %d", first.VersionNo) }
	second, err := store.CreatePriceVersion(ctx, menu.ID, 3000, "OMR")
	if err != nil { t.Fatal(err) }
	if second.VersionNo != 2 { t.Fatalf("second version number = %d", second.VersionNo) }
	current, err := store.GetCurrentPrice(ctx, menu.ID)
	if err != nil || current.ID != second.ID { t.Fatalf("current price = %#v err=%v", current, err) }

	order, created, err := store.CreateOrderFromPrice(ctx, customerID, "price-order-1", menu.ID)
	if err != nil { t.Fatalf("create priced order: %v", err) }
	if !created || order.AmountMinor != 3000 || order.Currency != "OMR" || order.PriceVersionID == nil || *order.PriceVersionID != second.ID {
		t.Fatalf("order did not snapshot current price: %#v created=%v", order, created)
	}
	// A later menu version must not rewrite an existing order's price snapshot.
	third, err := store.CreatePriceVersion(ctx, menu.ID, 3500, "OMR")
	if err != nil { t.Fatal(err) }
	replayed, created, err := store.CreateOrderFromPrice(ctx, customerID, "price-order-1", menu.ID)
	if err != nil || created || replayed.ID != order.ID || replayed.PriceVersionID == nil || *replayed.PriceVersionID != second.ID {
		t.Fatalf("idempotent replay must retain original version: %#v created=%v err=%v", replayed, created, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE service_price_versions SET amount_minor=1 WHERE id=$1`, second.ID); err == nil {
		t.Fatal("price versions must be immutable")
	}
	if err = store.SetCategoryEnabled(ctx, category.ID, false); err != nil { t.Fatal(err) }
	disabledReplay, created, err := store.CreateOrderFromPrice(ctx, customerID, "price-order-1", menu.ID)
	if err != nil || created || disabledReplay.ID != order.ID {
		t.Fatalf("existing idempotent order must survive later category disable: %#v created=%v err=%v", disabledReplay, created, err)
	}
	if _, _, err = store.CreateOrderFromPrice(ctx, customerID, "price-order-2", menu.ID); !errors.Is(err, ErrPriceUnavailable) {
		t.Fatalf("disabled category must reject new orders: %v", err)
	}
	if third.VersionNo != 3 { t.Fatalf("third version number = %d", third.VersionNo) }
}
