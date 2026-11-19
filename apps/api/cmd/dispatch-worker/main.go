package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketplace"
)

func main() {
	dsn:=os.Getenv("DATABASE_URL")
	if dsn=="" { log.Fatal("DATABASE_URL is required") }
	ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
	defer stop()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { log.Fatal(err) }
	defer pool.Close()
	if err=pool.Ping(ctx); err!=nil { log.Fatal(err) }

	store:=&marketplace.Store{DB:pool}
	ticker:=time.NewTicker(15*time.Second)
	defer ticker.Stop()
	log.Print("dispatch expiry worker started; batch=100 interval=15s")
	for {
		if _,err=store.ExpireDispatchOffers(ctx,100); err!=nil && ctx.Err()==nil {
			log.Printf("expire dispatch offers: %v",err)
		}
		select {
		case <-ctx.Done():
			log.Print("dispatch expiry worker stopped")
			return
		case <-ticker.C:
		}
	}
}
