// Package outbox implements leased, bounded-retry publication and idempotent consumption.
package outbox

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "regexp"
 "strconv"
 "strings"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "github.com/jackc/pgx/v5/pgxpool"
)

var (
 ErrInvalidPolicy=errors.New("invalid outbox policy")
 ErrLeaseLost=errors.New("outbox event lease was lost")
)

type Event struct { ID int64; AggregateType string; AggregateID string; EventType string; Payload json.RawMessage; Attempts int }
type Publisher interface { Publish(context.Context,Event,string) error }
type PublishFunc func(context.Context,Event,string) error
func (f PublishFunc) Publish(ctx context.Context,e Event,key string) error { return f(ctx,e,key) }

type Transaction interface { QueryRow(context.Context,string,...any) pgx.Row }

type EventAggregate struct {
 Type string `json:"type"`
 ID string `json:"id"`
}

// DomainEvent is the canonical v1 tenant-scoped envelope.
type DomainEvent struct {
 ID string `json:"id"`
 Type string `json:"type"`
 Version int `json:"version"`
 OccurredAt time.Time `json:"occurred_at"`
 TenantID string `json:"tenant_id"`
 Aggregate EventAggregate `json:"aggregate"`
 CorrelationID string `json:"correlation_id,omitempty"`
 CausationID string `json:"causation_id,omitempty"`
 Data json.RawMessage `json:"data"`
 Metadata map[string]string `json:"metadata,omitempty"`
}

var domainEventTypePattern = regexp.MustCompile("^[a-z][a-z0-9]*(\\.[a-z][a-z0-9_]*)+$")

// Names are stored without separators so snake_case, kebab-case, dotted and
// camelCase variants receive the same privacy treatment.
var prohibitedEventFieldNames = []string{
 "phone", "phonenumber", "email", "address", "accesstoken", "refreshtoken",
 "token", "password", "secret", "evidenceref", "evidencereference",
 "nationalid", "identitydocument", "rawtext", "latitude", "longitude",
}

func prohibitedEventFieldName(key string) bool {
 normalized := strings.ToLower(strings.TrimSpace(key))
 normalized = strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(normalized)
 for _, prohibited := range prohibitedEventFieldNames {
  if strings.Contains(normalized, prohibited) {
   return true
  }
 }
 return false
}

func ValidateDomainEvent(event DomainEvent) error {
 if strings.TrimSpace(event.ID)=="" || len(event.ID)>200 ||
  !domainEventTypePattern.MatchString(event.Type) || len(event.Type)>160 ||
  event.Version<1 || event.OccurredAt.IsZero() ||
  strings.TrimSpace(event.TenantID)=="" || len(event.TenantID)>120 ||
  strings.TrimSpace(event.Aggregate.Type)=="" || len(event.Aggregate.Type)>80 ||
  strings.TrimSpace(event.Aggregate.ID)=="" || len(event.Aggregate.ID)>200 ||
  len(event.CorrelationID)>200 || len(event.CausationID)>200 ||
  len(event.Data)==0 || len(event.Data)>256*1024 || !json.Valid(event.Data) {
  return ErrInvalidPolicy
 }
 var data map[string]any
 if err:=json.Unmarshal(event.Data,&data); err!=nil || data==nil { return ErrInvalidPolicy }
 if containsProhibitedEventField(data) { return ErrInvalidPolicy }
 for key:=range event.Metadata {
  if prohibitedEventFieldName(key) { return ErrInvalidPolicy }
 }
 return nil
}

func containsProhibitedEventField(value any) bool {
 switch typed:=value.(type) {
 case map[string]any:
  for key,nested:=range typed {
   if prohibitedEventFieldName(key) { return true }
   if containsProhibitedEventField(nested) { return true }
  }
 case []any:
  for _,nested:=range typed { if containsProhibitedEventField(nested) { return true } }
 }
 return false
}

func AppendDomainEvent(ctx context.Context, tx Transaction, event DomainEvent) (int64,error) {
 if err:=ValidateDomainEvent(event); err!=nil { return 0,err }
 payload,err:=json.Marshal(event)
 if err!=nil { return 0,err }
 return Append(ctx,tx,event.Aggregate.Type,event.Aggregate.ID,event.Type,payload)
}

func Append(ctx context.Context,tx Transaction,aggregateType,aggregateID,eventType string,payload json.RawMessage)(int64,error) {
 if tx==nil||strings.TrimSpace(aggregateType)==""||strings.TrimSpace(aggregateID)==""||strings.TrimSpace(eventType)==""||len(payload)==0||len(payload)>256*1024||!json.Valid(payload){return 0,ErrInvalidPolicy}
 var id int64
 err:=tx.QueryRow(ctx,`INSERT INTO outbox_events(aggregate_type,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4::jsonb) RETURNING id`,aggregateType,aggregateID,eventType,[]byte(payload)).Scan(&id)
 return id,err
}

type Repository struct { DB *pgxpool.Pool }
func (r *Repository) Claim(ctx context.Context,workerID string,limit int,lease time.Duration)([]Event,error) {
 if r==nil||r.DB==nil||strings.TrimSpace(workerID)==""||limit<1||limit>500||lease<time.Second||lease>5*time.Minute{return nil,ErrInvalidPolicy}
 tx,err:=r.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return nil,err};defer tx.Rollback(ctx)
 rows,err:=tx.Query(ctx,`
 WITH candidates AS (
  SELECT id FROM outbox_events WHERE published_at IS NULL AND dead_lettered_at IS NULL
   AND next_attempt_at<=now() AND (locked_until IS NULL OR locked_until<now())
  ORDER BY id FOR UPDATE SKIP LOCKED LIMIT $1
 )
 UPDATE outbox_events e SET locked_by=$2,locked_until=now()+($3 * interval '1 second')
 FROM candidates c WHERE e.id=c.id
 RETURNING e.id,e.aggregate_type,e.aggregate_id,e.event_type,e.payload,e.attempts
 `,limit,workerID,lease.Seconds())
 if err!=nil{return nil,err}
 defer rows.Close()
 events:=make([]Event,0,limit)
 for rows.Next(){var e Event;if err=rows.Scan(&e.ID,&e.AggregateType,&e.AggregateID,&e.EventType,&e.Payload,&e.Attempts);err!=nil{return nil,err};events=append(events,e)}
 if err=rows.Err();err!=nil{return nil,err}
 if err=tx.Commit(ctx);err!=nil{return nil,err}
 return events,nil
}
func (r *Repository) MarkPublished(ctx context.Context,workerID string,eventID int64)error{
 tag,err:=r.DB.Exec(ctx,`UPDATE outbox_events SET published_at=now(),locked_by=NULL,locked_until=NULL,last_error=NULL WHERE id=$1 AND locked_by=$2 AND published_at IS NULL AND dead_lettered_at IS NULL`,eventID,workerID)
 if err!=nil{return err};if tag.RowsAffected()!=1{return ErrLeaseLost};return nil
}
func (r *Repository) MarkFailed(ctx context.Context,workerID string,eventID int64,maxAttempts int,retryAt time.Time,cause error,retryable bool)error{
 if maxAttempts<1||maxAttempts>20||cause==nil{return ErrInvalidPolicy}
 message:=cause.Error();if len(message)>500{message=message[:500]}
 var tag pgconn.CommandTag
 var err error
 if !retryable {
  tag,err=r.DB.Exec(ctx,`UPDATE outbox_events SET attempts=attempts+1,last_error=$3,dead_lettered_at=now(),locked_by=NULL,locked_until=NULL WHERE id=$1 AND locked_by=$2 AND published_at IS NULL AND dead_lettered_at IS NULL`,eventID,workerID,message)
 } else {
  tag,err=r.DB.Exec(ctx,`UPDATE outbox_events SET attempts=attempts+1,last_error=$3,next_attempt_at=CASE WHEN attempts+1 >= $4 THEN next_attempt_at ELSE $5 END,dead_lettered_at=CASE WHEN attempts+1 >= $4 THEN now() ELSE NULL END,locked_by=NULL,locked_until=NULL WHERE id=$1 AND locked_by=$2 AND published_at IS NULL AND dead_lettered_at IS NULL`,eventID,workerID,message,maxAttempts,retryAt.UTC())
 }
 if err!=nil{return err};if tag.RowsAffected()!=1{return ErrLeaseLost};return nil
}

type Worker struct {
 Repository *Repository
 Publisher Publisher
 WorkerID string
 BatchSize int
 Lease time.Duration
 MaxAttempts int
 BaseBackoff time.Duration
 MaxBackoff time.Duration
 Retryable func(error)bool
}
func (w Worker) RunOnce(ctx context.Context)(int,error){
 if w.Repository==nil||w.Publisher==nil||strings.TrimSpace(w.WorkerID)==""||w.BatchSize<1||w.BatchSize>500||
  w.Lease<time.Second||w.MaxAttempts<1||w.MaxAttempts>20||w.BaseBackoff<0||w.MaxBackoff<w.BaseBackoff||w.Retryable==nil{return 0,ErrInvalidPolicy}
 events,err:=w.Repository.Claim(ctx,w.WorkerID,w.BatchSize,w.Lease);if err!=nil{return 0,err}
 published:=0
 var failures []error
 for _,event:=range events {
  if err:=ctx.Err();err!=nil{return published,err}
  key:=strconv.FormatInt(event.ID,10)
  publishErr:=w.Publisher.Publish(ctx,event,key)
  if publishErr==nil {
   if err=w.Repository.MarkPublished(ctx,w.WorkerID,event.ID);err!=nil{failures=append(failures,fmt.Errorf("ack event %d: %w",event.ID,err));continue}
   published++;continue
  }
  retryable:=w.Retryable(publishErr)
  delay:=RetryDelay(event.Attempts+1,w.BaseBackoff,w.MaxBackoff)
  if err=w.Repository.MarkFailed(ctx,w.WorkerID,event.ID,w.MaxAttempts,time.Now().Add(delay),publishErr,retryable);err!=nil{failures=append(failures,fmt.Errorf("record failure for event %d: %w",event.ID,err))}
 }
 return published,errors.Join(failures...)
}
func RetryDelay(attempt int,base,max time.Duration)time.Duration{
 if attempt<1||base<=0||max<=0{return 0};if base>=max{return max}
 delay:=base
 for i:=1;i<attempt&&delay<max;i++{if delay>max/2{return max};delay*=2}
 if delay>max{return max};return delay
}

type ConsumerHandler func(context.Context,pgx.Tx,Event)error
type Consumer struct { DB *pgxpool.Pool; Name string; Handler ConsumerHandler }
func (c Consumer) Process(ctx context.Context,event Event)(bool,error){
 if c.DB==nil||strings.TrimSpace(c.Name)==""||event.ID<=0||c.Handler==nil{return false,ErrInvalidPolicy}
 tx,err:=c.DB.BeginTx(ctx,pgx.TxOptions{});if err!=nil{return false,err};defer tx.Rollback(ctx)
 tag,err:=tx.Exec(ctx,`INSERT INTO processed_events(consumer_name,event_id) VALUES($1,$2) ON CONFLICT DO NOTHING`,c.Name,event.ID)
 if err!=nil{return false,err}
 if tag.RowsAffected()==0{if err=tx.Commit(ctx);err!=nil{return false,err};return false,nil}
 if err=c.Handler(ctx,tx,event);err!=nil{return false,err}
 if err=tx.Commit(ctx);err!=nil{return false,err}
 return true,nil
}
