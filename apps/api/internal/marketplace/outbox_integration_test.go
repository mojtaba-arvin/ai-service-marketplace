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
  if _,e=pool.Exec(ctx,string(raw));e!=nil{t.Fatalf("apply %s: %v",name,e)}
 }
 tx,err:=pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{t.Fatal(err)}
 payload,_:=json.Marshal(map[string]string{"order_id":"outbox-test"})
 eventID,err:=outbox.Append(ctx,tx,"test","aggregate-"+fmt.Sprint(time.Now().UnixNano()),"test.created",payload)
 if err!=nil{t.Fatal(err)}
 if err=tx.Commit(ctx);err!=nil{t.Fatal(err)}
 repo:=&outbox.Repository{DB:pool}
 claimed,err:=repo.Claim(ctx,"marketplace-test-worker-a",10,30*time.Second);if err!=nil{t.Fatal(err)}
 var event *outbox.Event
 for i:=range claimed{if claimed[i].ID==eventID{event=&claimed[i];break}}
 if event==nil{t.Fatalf("event %d was not claimed",eventID)}
 other,err:=repo.Claim(ctx,"marketplace-test-worker-b",10,30*time.Second);if err!=nil{t.Fatal(err)}
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
}
