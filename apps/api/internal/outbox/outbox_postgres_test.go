package outbox

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
)

func TestOutboxLeasePublicationAndIdempotentConsumptionPostgres(t *testing.T) {
 dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL is not configured")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 root,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer root.Close()
 schema:=fmt.Sprintf("outbox_test_%d",time.Now().UnixNano())
 if _,err=root.Exec(ctx,"CREATE SCHEMA "+schema);err!=nil{t.Fatal(err)}
 defer func(){_,_=root.Exec(context.Background(),"DROP SCHEMA "+schema+" CASCADE")}()
 cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)}
 cfg.ConnConfig.RuntimeParams["search_path"]=schema
 pool,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer pool.Close()
 for _,name:=range []string{"001_walking_skeleton.sql","019_outbox_delivery.sql"}{
  raw,e:=os.ReadFile(filepath.Join("..","..","migrations",name));if e!=nil{t.Fatal(e)}
  if _,e=pool.Exec(ctx,string(raw));e!=nil{t.Fatalf("apply %s: %v",name,e)}
 }
 tx,err:=pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil{t.Fatal(err)}
 payload,_:=json.Marshal(map[string]string{"test":"outbox"})
 eventID,err:=Append(ctx,tx,"test","aggregate-"+fmt.Sprint(time.Now().UnixNano()),"test.event",payload)
 if err!=nil{t.Fatal(err)}
 if err=tx.Commit(ctx);err!=nil{t.Fatal(err)}
 repo:=&Repository{DB:pool}
 claimed,err:=repo.Claim(ctx,"worker-a",10,30*time.Second);if err!=nil{t.Fatal(err)}
 var found *Event
 for i:=range claimed{if claimed[i].ID==eventID{found=&claimed[i];break}}
 if found==nil{t.Fatalf("event %d was not claimed",eventID)}
 if _,err=repo.Claim(ctx,"worker-b",10,30*time.Second);err!=nil{t.Fatal(err)}
 if err=repo.MarkPublished(ctx,"worker-a",eventID);err!=nil{t.Fatal(err)}
 if err=repo.MarkPublished(ctx,"worker-b",eventID);err!=ErrLeaseLost{t.Fatalf("stale owner ack: %v",err)}
 if _,err=pool.Exec(ctx,`CREATE TABLE outbox_test_effects(event_id bigint PRIMARY KEY)`);err!=nil{t.Fatal(err)}
 consumer:=Consumer{DB:pool,Name:"test-consumer-"+fmt.Sprint(time.Now().UnixNano()),Handler:func(ctx context.Context,tx pgx.Tx,e Event)error{
  _,e2:=tx.Exec(ctx,`INSERT INTO outbox_test_effects(event_id) VALUES($1)`,e.ID)
  return e2
 }}
 first,err:=consumer.Process(ctx,*found);if err!=nil{t.Fatal(err)}
 second,err:=consumer.Process(ctx,*found);if err!=nil{t.Fatal(err)}
 if !first||second{t.Fatalf("expected first delivery to process and duplicate to skip: first=%v second=%v",first,second)}
 var effects int
 if err=pool.QueryRow(ctx,`SELECT count(*) FROM outbox_test_effects WHERE event_id=$1`,eventID).Scan(&effects);err!=nil{t.Fatal(err)}
 if effects!=1{t.Fatalf("expected one side effect, got %d",effects)}
}
