package marketplace

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/analytics"
    "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func TestAnalyticsProjectorStoresCanonicalFactsAndQuarantinesUnsafeEventsPostgres(t *testing.T) {
    dsn:=os.Getenv("TEST_DATABASE_URL")
    if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
    ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
    defer cancel()
    pool,err:=pgxpool.New(ctx,dsn)
    if err!=nil { t.Fatal(err) }
    defer pool.Close()
    for _,name:=range []string{"001_walking_skeleton.sql","019_outbox_delivery.sql","025_analytics_event_projection.sql"} {
        raw,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
        if readErr!=nil { t.Fatal(readErr) }
        if _,err=execMigrationScript(ctx,pool,raw);err!=nil { t.Fatalf("apply %s: %v",name,err) }
    }

    suffix:=fmt.Sprint(time.Now().UnixNano())
    aggregateID:="analytics-order-"+suffix
    domain:=outbox.DomainEvent{
        ID:"analytics-event-"+suffix,Type:"order.created",Version:1,
        OccurredAt:time.Date(2026,11,22,12,0,0,0,time.UTC),TenantID:"analytics-tenant-a",
        Aggregate:outbox.EventAggregate{Type:"order",ID:aggregateID},
        Metadata:map[string]string{"market_code":"OM","traffic_class":"production"},
        Data:json.RawMessage(`{"order_id":"`+aggregateID+`","status":"requested","price_version_id":"price-v1","scheduled_at":"2026-11-23T10:00:00Z"}`),
    }
    tx,err:=pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil { t.Fatal(err) }
    canonicalID,err:=outbox.AppendDomainEvent(ctx,tx,domain)
    if err!=nil { _=tx.Rollback(ctx);t.Fatal(err) }
    if err=tx.Commit(ctx);err!=nil { t.Fatal(err) }

    legacyTx,err:=pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil { t.Fatal(err) }
    legacyPayload:=json.RawMessage(`{"order_id":"legacy-`+suffix+`","phone":"+96890000000"}`)
    legacyID,err:=outbox.Append(ctx,legacyTx,"order","legacy-"+suffix,"order.created",legacyPayload)
    if err!=nil { _=legacyTx.Rollback(ctx);t.Fatal(err) }
    if err=legacyTx.Commit(ctx);err!=nil { t.Fatal(err) }

    unsafeTx,err:=pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil { t.Fatal(err) }
    unsafeDomain:=domain
    unsafeDomain.ID="analytics-unsafe-"+suffix
    unsafeDomain.Aggregate=outbox.EventAggregate{Type:"order",ID:"unsafe-"+suffix}
    unsafeDomain.Data=json.RawMessage(`{"order_id":"unsafe-`+suffix+`","status":"requested","phoneNumber":"+96890000000"}`)
    unsafeID,err:=outbox.Append(ctx,unsafeTx,"order","unsafe-"+suffix,"order.created",mustJSON(t,unsafeDomain))
    if err!=nil { _=unsafeTx.Rollback(ctx);t.Fatal(err) }
    if err=unsafeTx.Commit(ctx);err!=nil { t.Fatal(err) }

    projector:=analytics.Projector{DB:pool,ConsumerName:"analytics-facts-v1-"+suffix}
    consumer:=projector.Consumer()
    for _,id:=range []int64{canonicalID,legacyID,unsafeID} {
        var event outbox.Event
        if err=pool.QueryRow(ctx,`SELECT id,aggregate_type,aggregate_id,event_type,payload,attempts FROM outbox_events WHERE id=$1`,id).
            Scan(&event.ID,&event.AggregateType,&event.AggregateID,&event.EventType,&event.Payload,&event.Attempts);err!=nil { t.Fatal(err) }
        applied,processErr:=consumer.Process(ctx,event)
        if processErr!=nil || !applied { t.Fatalf("project event %d: applied=%v err=%v",id,applied,processErr) }
    }

    offerID:="analytics-offer-"+suffix
    offerEvent:=outbox.DomainEvent{
        ID:"analytics-offer-event-"+suffix,Type:"dispatch.offer_created",Version:1,
        OccurredAt:time.Date(2026,11,22,12,1,0,0,time.UTC),TenantID:domain.TenantID,
        Aggregate:outbox.EventAggregate{Type:"dispatch_offer",ID:offerID},
        Metadata:map[string]string{"market_code":"OM","traffic_class":"production"},
        Data:json.RawMessage(`{"offer_id":"`+offerID+`","order_id":"`+aggregateID+`","provider_id":"provider-1","expires_at":"2026-11-22T12:30:00Z"}`),
    }
    assignmentEvent:=outbox.DomainEvent{
        ID:"analytics-assignment-event-"+suffix,Type:"order.provider_assigned",Version:1,
        OccurredAt:time.Date(2026,11,22,12,10,0,0,time.UTC),TenantID:domain.TenantID,
        Aggregate:outbox.EventAggregate{Type:"order",ID:aggregateID},
        Metadata:map[string]string{"market_code":"OM","traffic_class":"production"},
        Data:json.RawMessage(`{"order_id":"`+aggregateID+`","provider_id":"provider-1","assignment_id":"assignment-1","offer_id":"`+offerID+`"}`),
    }
    for _,item:=range []outbox.DomainEvent{offerEvent,assignmentEvent} {
        eventTx,beginErr:=pool.BeginTx(ctx,pgx.TxOptions{});if beginErr!=nil { t.Fatal(beginErr) }
        id,appendErr:=outbox.AppendDomainEvent(ctx,eventTx,item)
        if appendErr!=nil { _=eventTx.Rollback(ctx);t.Fatal(appendErr) }
        if err=eventTx.Commit(ctx);err!=nil { t.Fatal(err) }
        var stored outbox.Event
        if err=pool.QueryRow(ctx,`SELECT id,aggregate_type,aggregate_id,event_type,payload,attempts FROM outbox_events WHERE id=$1`,id).
            Scan(&stored.ID,&stored.AggregateType,&stored.AggregateID,&stored.EventType,&stored.Payload,&stored.Attempts);err!=nil { t.Fatal(err) }
        if applied,processErr:=consumer.Process(ctx,stored);processErr!=nil || !applied {
            t.Fatalf("project %s: applied=%v err=%v",item.Type,applied,processErr)
        }
    }
    unclassified:=outbox.DomainEvent{
        ID:"analytics-unclassified-"+suffix,Type:"order.state_changed",Version:1,
        OccurredAt:time.Date(2026,11,22,12,20,0,0,time.UTC),TenantID:domain.TenantID,
        Aggregate:outbox.EventAggregate{Type:"order",ID:aggregateID},
        Data:json.RawMessage(`{"order_id":"`+aggregateID+`","from":"requested","to":"in_progress"}`),
    }
    unclassifiedTx,err:=pool.BeginTx(ctx,pgx.TxOptions{});if err!=nil { t.Fatal(err) }
    unclassifiedID,err:=outbox.AppendDomainEvent(ctx,unclassifiedTx,unclassified)
    if err!=nil { _=unclassifiedTx.Rollback(ctx);t.Fatal(err) }
    if err=unclassifiedTx.Commit(ctx);err!=nil { t.Fatal(err) }
    var unclassifiedStored outbox.Event
    if err=pool.QueryRow(ctx,`SELECT id,aggregate_type,aggregate_id,event_type,payload,attempts FROM outbox_events WHERE id=$1`,unclassifiedID).
        Scan(&unclassifiedStored.ID,&unclassifiedStored.AggregateType,&unclassifiedStored.AggregateID,&unclassifiedStored.EventType,&unclassifiedStored.Payload,&unclassifiedStored.Attempts);err!=nil { t.Fatal(err) }
    if applied,processErr:=consumer.Process(ctx,unclassifiedStored);processErr!=nil || !applied {
        t.Fatalf("project unclassified event: applied=%v err=%v",applied,processErr)
    }

    summary,err:=analytics.QuerySummary(ctx,pool,domain.TenantID,analytics.SummaryFilter{
        From:time.Date(2026,11,22,11,0,0,0,time.UTC),To:time.Date(2026,11,22,13,0,0,0,time.UTC),MarketCode:"OM",
    })
    if err!=nil { t.Fatal(err) }
    if summary.OrdersCreated!=1 || summary.OffersCreated!=1 || summary.OffersAccepted!=1 ||
        summary.OfferAcceptanceRate!=1 || summary.MedianAssignmentSeconds!=600 || summary.P90AssignmentSeconds!=600 ||
        !summary.DataReady || summary.EligibleEvents!=3 || summary.ExcludedEvents!=1 {
        t.Fatalf("unexpected tenant analytics summary: %+v",summary)
    }
    otherSummary,err:=analytics.QuerySummary(ctx,pool,"analytics-tenant-b",analytics.SummaryFilter{
        From:time.Date(2026,11,22,11,0,0,0,time.UTC),To:time.Date(2026,11,22,13,0,0,0,time.UTC),MarketCode:"OM",
    })
    if err!=nil || otherSummary.OrdersCreated!=0 || otherSummary.OffersCreated!=0 || otherSummary.DataReady {
        t.Fatalf("analytics summary must remain tenant-scoped: %+v err=%v",otherSummary,err)
    }
    if _,err=analytics.QuerySummary(ctx,pool,domain.TenantID,analytics.SummaryFilter{From:time.Now().UTC(),To:time.Now().UTC().Add(91*24*time.Hour)}); !errors.Is(err,analytics.ErrInvalidSummaryFilter) {
        t.Fatalf("analytics query must bound time range: %v",err)
    }
    var facts int
    var market,traffic string
    var projectedData []byte
    if err=pool.QueryRow(ctx,`SELECT count(*),min(market_code),min(traffic_class),min(data::text)::jsonb
        FROM analytics_event_facts WHERE tenant_id=$1 AND event_id=$2`,domain.TenantID,domain.ID).
        Scan(&facts,&market,&traffic,&projectedData);err!=nil { t.Fatal(err) }
    if facts!=1 || market!="OM" || traffic!="production" { t.Fatalf("canonical fact metadata mismatch: facts=%d market=%q traffic=%q",facts,market,traffic) }
    var data map[string]any
    if err=json.Unmarshal(projectedData,&data);err!=nil { t.Fatal(err) }
    if _,exists:=data["phoneNumber"];exists { t.Fatalf("unsafe field reached analytics fact: %#v",data) }

    var quarantined int
    if err=pool.QueryRow(ctx,`SELECT count(*) FROM analytics_event_quarantine WHERE outbox_event_id=ANY($1::bigint[])`,[]int64{legacyID,unsafeID}).Scan(&quarantined);err!=nil { t.Fatal(err) }
    if quarantined!=2 { t.Fatalf("legacy and unsafe events must be quarantined, got %d",quarantined) }
    var storedHash string
    if err=pool.QueryRow(ctx,`SELECT payload_sha256 FROM analytics_event_quarantine WHERE outbox_event_id=$1`,unsafeID).Scan(&storedHash);err!=nil { t.Fatal(err) }
    if len(storedHash)!=64 { t.Fatalf("quarantine must store only a SHA-256 fingerprint, got %q",storedHash) }

    var otherTenantFacts int
    if err=pool.QueryRow(ctx,`SELECT count(*) FROM analytics_event_facts WHERE tenant_id='analytics-tenant-b' AND event_id=$1`,domain.ID).Scan(&otherTenantFacts);err!=nil { t.Fatal(err) }
    if otherTenantFacts!=0 { t.Fatalf("analytics fact crossed tenant boundary: %d",otherTenantFacts) }
    if _,err=pool.Exec(ctx,`UPDATE analytics_event_facts SET traffic_class='test' WHERE tenant_id=$1 AND event_id=$2`,domain.TenantID,domain.ID);err==nil {
        t.Fatal("analytics facts must be append-only")
    }

    replayed,err:=consumer.Process(ctx,outbox.Event{ID:canonicalID,AggregateType:"order",AggregateID:aggregateID,EventType:"order.created",Payload:mustJSON(t,domain)})
    if err!=nil || replayed { t.Fatalf("duplicate canonical event must be idempotent: applied=%v err=%v",replayed,err) }
}

func mustJSON(t *testing.T,value any) []byte {
    t.Helper()
    raw,err:=json.Marshal(value)
    if err!=nil { t.Fatal(err) }
    return raw
}
