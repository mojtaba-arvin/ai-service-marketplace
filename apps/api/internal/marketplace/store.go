package marketplace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

type Store struct { DB *pgxpool.Pool }

type Order struct {
	ID string `json:"id"`
	CustomerID string `json:"customer_id"`
	AmountMinor int64 `json:"amount_minor"`
	Currency string `json:"currency"`
	Status string `json:"status"`
	PriceVersionID *string `json:"price_version_id,omitempty"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	CountryCode *string `json:"country_code,omitempty"`
	RegionCode *string `json:"region_code,omitempty"`
	CityCode *string `json:"city_code,omitempty"`
	AreaCode *string `json:"area_code,omitempty"`
}

func newID() (string,error) {
	var b [16]byte
	if _,err:=rand.Read(b[:]); err!=nil { return "",err }
	return hex.EncodeToString(b[:]),nil
}

func (s *Store) CreateOrder(ctx context.Context, customerID, key string, amount int64, currency string) (Order,bool,error) {
	if customerID=="" || key=="" { return Order{},false,errors.New("customer_id and Idempotency-Key are required") }
	if len(key)>200 { return Order{},false,errors.New("Idempotency-Key is too long") }
	if err:=ValidateAmount(amount,currency); err!=nil { return Order{},false,err }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return Order{},false,err }
	defer tx.Rollback(ctx)

	id,err:=newID(); if err!=nil { return Order{},false,err }
	var createdID string
	err=tx.QueryRow(ctx,`
		INSERT INTO orders(id,customer_id,idempotency_key,amount_minor,currency,status)
		VALUES($1,$2,$3,$4,$5,'requested')
		ON CONFLICT (customer_id,idempotency_key) DO NOTHING
		RETURNING id
	`,id,customerID,key,amount,currency).Scan(&createdID)
	if errors.Is(err,pgx.ErrNoRows) {
		var existing Order
		err=tx.QueryRow(ctx,`SELECT id,customer_id,amount_minor,currency,status FROM orders WHERE customer_id=$1 AND idempotency_key=$2`,customerID,key).
			Scan(&existing.ID,&existing.CustomerID,&existing.AmountMinor,&existing.Currency,&existing.Status)
		if err!=nil { return Order{},false,err }
		if existing.AmountMinor!=amount || existing.Currency!=currency { return Order{},false,errors.New("idempotency key reused with different request") }
		if err=tx.Commit(ctx); err!=nil { return Order{},false,err }
		return existing,false,nil
	}
	if err!=nil { return Order{},false,err }

	// This is a simulated test ledger, not a payment authorization or escrow.
	var journalID string
	err=tx.QueryRow(ctx,`INSERT INTO ledger_journals(id,idempotency_key,currency,description)
		VALUES($1,$2,$3,'walking-skeleton simulated order hold')
		ON CONFLICT(idempotency_key) DO NOTHING RETURNING id`,id,"order:"+id,currency).Scan(&journalID)
	if err!=nil { return Order{},false,fmt.Errorf("create test ledger journal: %w",err) }
	_,err=tx.Exec(ctx,`INSERT INTO ledger_entries(journal_id,account_code,side,amount_minor)
		VALUES($1,'customer_funds','debit',$2),($1,'provider_payable','credit',$2)`,journalID,amount)
	if err!=nil { return Order{},false,fmt.Errorf("post balanced test ledger: %w",err) }

	payload:=[]byte(`{"order_id":"`+id+`","status":"requested"}`)
	_,err=outbox.Append(ctx,tx,"order",id,"order.created",payload)
	if err!=nil { return Order{},false,err }
	if err=tx.Commit(ctx); err!=nil { return Order{},false,err }
	return Order{ID:id,CustomerID:customerID,AmountMinor:amount,Currency:currency,Status:"requested"},true,nil
}

func (s *Store) TransitionOrder(ctx context.Context, id, target string) (Order,error) {
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return Order{},err }
	defer tx.Rollback(ctx)
	var current Order
	err=tx.QueryRow(ctx,`SELECT id,customer_id,amount_minor,currency,status,price_version_id,scheduled_at,country_code,region_code,city_code,area_code FROM orders WHERE id=$1 FOR UPDATE`,id).
		Scan(&current.ID,&current.CustomerID,&current.AmountMinor,&current.Currency,&current.Status,&current.PriceVersionID,&current.ScheduledAt,&current.CountryCode,&current.RegionCode,&current.CityCode,&current.AreaCode)
	if err!=nil { return Order{},err }
	if current.Status==target { if err=tx.Commit(ctx); err!=nil{return Order{},err}; return current,nil }
	if !CanTransition(current.Status,target) { return Order{},ErrInvalidTransition }
	_,err=tx.Exec(ctx,`UPDATE orders SET status=$2,updated_at=now() WHERE id=$1`,id,target)
	if err!=nil { return Order{},err }
	payload:=[]byte(`{"order_id":"`+id+`","from":"`+current.Status+`","to":"`+target+`"}`)
	_,err=outbox.Append(ctx,tx,"order",id,"order.state_changed",payload)
	if err!=nil { return Order{},err }
	current.Status=target
	if err=tx.Commit(ctx); err!=nil { return Order{},err }
	return current,nil
}

func (s *Store) Health(ctx context.Context) error {
	ctx,cancel:=context.WithTimeout(ctx,2*time.Second); defer cancel()
	return s.DB.Ping(ctx)
}
