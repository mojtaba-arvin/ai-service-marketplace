package main

import (
 "context"
 "log"
 "net/http"
 "os"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/marketplace"
)

func requiredSecret(name string) []byte {
 value:=os.Getenv(name)
 if len(value)<32 { log.Fatalf("%s must contain at least 32 bytes",name) }
 return []byte(value)
}

func main() {
 dsn:=os.Getenv("DATABASE_URL")
 if dsn=="" {log.Fatal("DATABASE_URL is required")}
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{log.Fatal(err)};defer pool.Close()
 if err=pool.Ping(ctx);err!=nil{log.Fatal(err)}
 migrations:=[]string{
  "001_walking_skeleton.sql","002_identity.sql","003_sessions.sql","004_catalog.sql",
  "005_pricing.sql","006_provider_eligibility.sql","007_market_config.sql","008_catalog_localization.sql",
  "009_ledger_accounts.sql","010_ledger_idempotency_reversal.sql","011_payment_adapter_contract.sql",
  "012_settlement_reconciliation.sql","013_inspection_quotes.sql","014_quote_variations.sql",
  "015_order_schedule.sql","016_cancellation_policy.sql","017_provider_availability.sql",
  "018_dispatch_offers.sql","019_outbox_delivery.sql","020_professional_verification.sql","021_support_cases.sql",
 }
 for _,name:=range migrations {
  migration,readErr:=os.ReadFile("migrations/"+name);if readErr!=nil{log.Fatal(readErr)}
  if _,migrateErr:=pool.Exec(ctx,string(migration));migrateErr!=nil{log.Fatalf("apply %s: %v",name,migrateErr)}
 }
 store:=&marketplace.Store{DB:pool}
 appEnv:=os.Getenv("APP_ENV")
 var handler *marketplace.Handler
 if appEnv=="production" {
  delivery,deliveryErr:=marketplace.NewHTTPOTPDelivery(os.Getenv("OTP_DELIVERY_URL"),os.Getenv("OTP_DELIVERY_TOKEN"),&http.Client{Timeout:8*time.Second})
  if deliveryErr!=nil{log.Fatalf("configure OTP delivery: %v",deliveryErr)}
  handler=&marketplace.Handler{
   Store:store,OTPPepper:requiredSecret("OTP_PEPPER"),SessionPepper:requiredSecret("SESSION_PEPPER"),
   OTPDelivery:delivery,AccessTTL:15*time.Minute,RefreshTTL:30*24*time.Hour,
  }
 } else {
  code:=os.Getenv("TEST_OTP_CODE");if code==""{code="000000"}
  handler=&marketplace.Handler{Store:store,OTPCode:code,TestMode:true}
 }
 addr:=os.Getenv("HTTP_ADDR");if addr==""{addr=":8080"}
 server:=&http.Server{Addr:addr,Handler:handler,ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,WriteTimeout:10*time.Second,IdleTimeout:60*time.Second}
 log.Printf("marketplace API listening on %s (environment=%s)",addr,appEnv)
 log.Fatal(server.ListenAndServe())
}
