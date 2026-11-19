package marketplace

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxEvent struct {
	ID int64
	AggregateType string
	AggregateID string
	EventType string
	Payload json.RawMessage
}

// ProcessOutboxBatch claims pending rows with SKIP LOCKED. Handlers must be idempotent
// by (consumerName,event.ID), because a process can crash after an external side effect
// but before this transaction commits.
func ProcessOutboxBatch(ctx context.Context, db *pgxpool.Pool, consumerName string, limit int, handler func(context.Context, OutboxEvent) error) (int,error) {
	if consumerName=="" || limit<1 || limit>100 || handler==nil { return 0,fmt.Errorf("consumer name, handler, and limit 1..100 are required") }
	tx,err:=db.BeginTx(ctx,pgx.TxOptions{}); if err!=nil { return 0,err }; defer tx.Rollback(ctx)
	rows,err:=tx.Query(ctx,`SELECT id,aggregate_type,aggregate_id,event_type,payload FROM outbox_events
		WHERE published_at IS NULL ORDER BY id FOR UPDATE SKIP LOCKED LIMIT $1`,limit)
	if err!=nil { return 0,err }
	events:=make([]OutboxEvent,0,limit)
	for rows.Next() {
		var event OutboxEvent
		if err=rows.Scan(&event.ID,&event.AggregateType,&event.AggregateID,&event.EventType,&event.Payload); err!=nil { rows.Close(); return 0,err }
		events=append(events,event)
	}
	rows.Close()
	if err=rows.Err(); err!=nil { return 0,err }
	processed:=0
	for _,event:=range events {
		tag,insertErr:=tx.Exec(ctx,`INSERT INTO processed_events(consumer_name,event_id)
			VALUES($1,$2) ON CONFLICT DO NOTHING`,consumerName,event.ID)
		if insertErr!=nil { return processed,insertErr }
		if tag.RowsAffected()==0 {
			_,err=tx.Exec(ctx,`UPDATE outbox_events SET published_at=now() WHERE id=$1`,event.ID)
			if err!=nil { return processed,err }
			continue
		}
		if err=handler(ctx,event); err!=nil { return processed,fmt.Errorf("handle outbox event %d: %w",event.ID,err) }
		_,err=tx.Exec(ctx,`UPDATE outbox_events SET published_at=now() WHERE id=$1`,event.ID)
		if err!=nil { return processed,err }
		processed++
	}
	if err=tx.Commit(ctx); err!=nil { return 0,err }
	return processed,nil
}
