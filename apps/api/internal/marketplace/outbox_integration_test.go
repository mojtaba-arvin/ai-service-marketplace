package marketplace

import (
 "context"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "testing"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func TestOutboxLeasesAndConsumerIdempotencyPostgres(t *testing.T){
 dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 pool,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer pool.Close()
 for _,name:=range []string{"001_walking_skeleton.sql","019_outbox_delivery.sql"}{
  raw,e:=os.ReadFile(filepath.Join("..","..","migrations",name));if e!=nil{t.Fatal(e)}
  if _,e=execMigrationScript(ctx, pool, raw);e!=nil{t.Fatalf("apply %s: %v",name,e)}
 }
 tx,err:=pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{t.Fatal(err)}
 payload,_:=json.Marshal(map[string]string{"order_id":"outbox-test"})
 eventID,err:=outbox.Append(ctx,tx,"test","aggregate-"+fmt.Sprint(time.Now().UnixNano()),"test.created",payload)
 if err!=nil{t.Fatal(err)}
 if err=tx.Commit(ctx);err!=nil{t.Fatal(err)}
 repo:=&outbox.Repository{DB:pool}
 claimed,err:=repo.Claim(ctx,"marketplace-test-worker-a",500,30*time.Second);if err!=nil{t.Fatal(err)}
 var event *outbox.Event
 for i:=range claimed{if claimed[i].ID==eventID{event=&claimed[i];break}}
 if event==nil{t.Fatalf("event %d was not claimed",eventID)}
 other,err:=repo.Claim(ctx,"marketplace-test-worker-b",500,30*time.Second);if err!=nil{t.Fatal(err)}
 for _,candidate:=range other{if candidate.ID==eventID{t.Fatal("two workers claimed the same leased event")}}
 if err=repo.MarkPublished(ctx,"marketplace-test-worker-a",eventID);err!=nil{t.Fatal(err)}
 if err=repo.MarkPublished(ctx,"marketplace-test-worker-b",eventID);err!=outbox.ErrLeaseLost{t.Fatalf("stale worker acknowledgement: %v",err)}
 if _,err=pool.Exec(ctx,`CREATE TABLE IF NOT EXISTS outbox_test_effects(event_id bigint PRIMARY KEY)`);err!=nil{t.Fatal(err)}
 consumer:=outbox.Consumer{DB:pool,Name:"marketplace-test-consumer",Handler:func(ctx context.Context,tx pgx.Tx,e outbox.Event)error{
  _,e2:=tx.Exec(ctx,`INSERT INTO outbox_test_effects(event_id) VALUES($1) ON CONFLICT DO NOTHING`,e.ID)
  return e2
 }}
 first,err:=consumer.Process(ctx,*event);if err!=nil{t.Fatal(err)}
 second,err:=consumer.Process(ctx,*event);if err!=nil{t.Fatal(err)}
 if !first||second{t.Fatalf("expected one processing pass: first=%v second=%v",first,second)}
 var effects int
 if err=pool.QueryRow(ctx,`SELECT count(*) FROM outbox_test_effects WHERE event_id=$1`,eventID).Scan(&effects);err!=nil{t.Fatal(err)}
 if effects!=1{t.Fatalf("expected one consumer side effect, got %d",effects)}


	// Replay a bounded range from the durable outbox using a new projection
	// generation. Repeating the same range must not repeat consumer effects.
	var beforeReplayID int64
	if err=pool.QueryRow(ctx,`SELECT COALESCE(MAX(id),0) FROM outbox_events`).Scan(&beforeReplayID); err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`CREATE TABLE IF NOT EXISTS outbox_test_replay_effects(event_id bigint PRIMARY KEY)`); err!=nil { t.Fatal(err) }
	var replayEvents []outbox.Event
	for i:=0;i<2;i++ {
		tx,beginErr:=pool.BeginTx(ctx,pgx.TxOptions{});if beginErr!=nil { t.Fatal(beginErr) }
		payload,_:=json.Marshal(map[string]any{"sequence":i})
		aggregateID:=fmt.Sprintf("replay-%d-%d",time.Now().UnixNano(),i)
		id,appendErr:=outbox.Append(ctx,tx,"replay_test",aggregateID,"replay.test_event",payload)
		if appendErr!=nil { _=tx.Rollback(ctx);t.Fatal(appendErr) }
		if err=tx.Commit(ctx);err!=nil { t.Fatal(err) }
		replayEvents=append(replayEvents,outbox.Event{ID:id,AggregateType:"replay_test",AggregateID:aggregateID,EventType:"replay.test_event",Payload:payload})
	}
	replayConsumer:=outbox.Consumer{DB:pool,Name:"analytics-replay-generation-1",Handler:func(ctx context.Context,tx pgx.Tx,e outbox.Event)error{
		_,insertErr:=tx.Exec(ctx,`INSERT INTO outbox_test_replay_effects(event_id) VALUES($1) ON CONFLICT DO NOTHING`,e.ID)
		return insertErr
	}}
	applied,cursor,err:=replayConsumer.ReplayBatch(ctx,beforeReplayID,1)
	if err!=nil || applied!=1 || cursor!=replayEvents[0].ID { t.Fatalf("first replay batch: applied=%d cursor=%d err=%v",applied,cursor,err) }
	applied,cursor,err=replayConsumer.ReplayBatch(ctx,cursor,1)
	if err!=nil || applied!=1 || cursor!=replayEvents[1].ID { t.Fatalf("second replay batch: applied=%d cursor=%d err=%v",applied,cursor,err) }
	applied,cursor,err=replayConsumer.ReplayBatch(ctx,cursor,1)
	if err!=nil || applied!=0 || cursor!=replayEvents[1].ID { t.Fatalf("empty replay batch: applied=%d cursor=%d err=%v",applied,cursor,err) }
	applied,cursor,err=replayConsumer.ReplayBatch(ctx,beforeReplayID,10)
	if err!=nil || applied!=0 || cursor!=replayEvents[1].ID { t.Fatalf("replayed generation must deduplicate: applied=%d cursor=%d err=%v",applied,cursor,err) }
	var replayEffects int
	if err=pool.QueryRow(ctx,`SELECT count(*) FROM outbox_test_replay_effects WHERE event_id=ANY($1::bigint[])`,[]int64{replayEvents[0].ID,replayEvents[1].ID}).Scan(&replayEffects);err!=nil { t.Fatal(err) }
	if replayEffects!=2 { t.Fatalf("replay must apply each event once, got %d",replayEffects) }

	// Consumers may receive events out of order; the stable event identity
	// deduplicates them independently of delivery order.
	seen:=make([]int64,0,2)
	reordered:=outbox.Consumer{DB:pool,Name:"analytics-reordered-generation-1",Handler:func(_ context.Context,_ pgx.Tx,e outbox.Event)error{
		seen=append(seen,e.ID);return nil
	}}
	for i:=len(replayEvents)-1;i>=0;i-- {
		applied,processErr:=reordered.Process(ctx,replayEvents[i])
		if processErr!=nil || !applied { t.Fatalf("out-of-order event processing failed: applied=%v err=%v",applied,processErr) }
	}
	if len(seen)!=2 || seen[0]!=replayEvents[1].ID || seen[1]!=replayEvents[0].ID {
		t.Fatalf("consumer must tolerate reversed event delivery: %v",seen)
	}
}
