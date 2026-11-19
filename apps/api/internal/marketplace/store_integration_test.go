package marketplace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresWalkingSkeleton(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second); defer cancel()
	pool,err:=pgxpool.New(ctx,dsn); if err!=nil { t.Fatal(err) }; defer pool.Close()
	if err=pool.Ping(ctx); err!=nil { t.Fatal(err) }
	for _, name := range []string{"001_walking_skeleton.sql", "004_catalog.sql", "005_pricing.sql","015_order_schedule.sql"} {
		migration, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil { t.Fatal(readErr) }
		if _, err = pool.Exec(ctx, string(migration)); err != nil { t.Fatalf("migration %s: %v", name, err) }
	}
	store:=&Store{DB:pool}
	key:="integration-"+time.Now().UTC().Format("20060102150405.000000000")
	first,created,err:=store.CreateOrder(ctx,"integration-customer",key,1250,"OMR")
	if err!=nil { t.Fatalf("create order: %v",err) }
	if !created || first.Status!="requested" { t.Fatalf("unexpected first order: %#v created=%v",first,created) }
	replayed,created,err:=store.CreateOrder(ctx,"integration-customer",key,1250,"OMR")
	if err!=nil { t.Fatalf("idempotent replay: %v",err) }
	if created || replayed.ID!=first.ID { t.Fatalf("idempotency failed: %#v created=%v",replayed,created) }
	if _,_,err=store.CreateOrder(ctx,"integration-customer",key,1300,"OMR"); err==nil { t.Fatal("expected changed-payload idempotency conflict") }
	accepted,err:=store.TransitionOrder(ctx,first.ID,"accepted")
	if err!=nil || accepted.Status!="accepted" { t.Fatalf("valid transition failed: %#v %v",accepted,err) }
	if _,err=store.TransitionOrder(ctx,first.ID,"completed"); err!=ErrInvalidTransition { t.Fatalf("expected invalid transition, got %v",err) }

	var debits,credits int64
	if err=pool.QueryRow(ctx,`SELECT COALESCE(SUM(amount_minor) FILTER (WHERE side='debit'),0), COALESCE(SUM(amount_minor) FILTER (WHERE side='credit'),0) FROM ledger_entries WHERE journal_id=$1`,first.ID).Scan(&debits,&credits); err!=nil { t.Fatal(err) }
	if debits!=1250 || credits!=1250 { t.Fatalf("unbalanced ledger: debit=%d credit=%d",debits,credits) }
	var events int
	if err=pool.QueryRow(ctx,`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id=$1`,first.ID).Scan(&events); err!=nil { t.Fatal(err) }
	if events!=2 { t.Fatalf("expected order-created and state-changed events, got %d",events) }
	if _,err=pool.Exec(ctx,`UPDATE ledger_entries SET amount_minor=1 WHERE journal_id=$1`,first.ID); err==nil { t.Fatal("ledger mutation must be rejected") }

	calls, ownEvents := 0, 0
	processed,err:=ProcessOutboxBatch(ctx,pool,"integration-test",100,func(_ context.Context,event OutboxEvent) error {
		calls++
		if event.AggregateID==first.ID { ownEvents++ }
		return nil
	})
	if err!=nil || processed<2 || calls!=processed || ownEvents!=2 { t.Fatalf("outbox processing: processed=%d calls=%d own_events=%d err=%v",processed,calls,ownEvents,err) }
	processed,err=ProcessOutboxBatch(ctx,pool,"integration-test",100,func(context.Context,OutboxEvent) error {
		t.Fatal("already processed event was delivered twice")
		return nil
	})
	if err!=nil || processed!=0 { t.Fatalf("outbox replay: processed=%d err=%v",processed,err) }
}
