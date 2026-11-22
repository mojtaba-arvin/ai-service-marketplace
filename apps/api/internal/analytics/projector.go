package analytics

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "regexp"
    "strings"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

type Projector struct {
    DB *pgxpool.Pool
    ConsumerName string
}

var marketCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)

var eventDataFields = map[string]map[string]struct{}{
    "order.created": set("order_id","status","price_version_id","scheduled_at"),
    "order.state_changed": set("order_id","from","to"),
    "order.provider_assigned": set("order_id","provider_id","assignment_id","offer_id"),
    "order.cancelled": set("order_id","from","to","cancellation_id"),
    "provider.booking_reserved": set("booking_id","provider_id","order_id","start_at","end_at"),
    "provider.booking_cancelled": set("order_id","cancellation_id","status"),
    "dispatch.offer_created": set("offer_id","order_id","provider_id","expires_at"),
    "dispatch.offer_declined": set("offer_id","order_id","provider_id"),
    "dispatch.offer_expired": set("offer_id","order_id"),
}

func set(names ...string) map[string]struct{} {
    result:=make(map[string]struct{},len(names))
    for _,name:=range names { result[name]=struct{}{} }
    return result
}

// Consumer returns an outbox consumer whose name identifies one immutable
// projection generation. Use a new name to rebuild after a schema or logic change.
func (p Projector) Consumer() outbox.Consumer {
    return outbox.Consumer{DB:p.DB,Name:p.ConsumerName,Handler:p.Handle}
}

func (p Projector) Handle(ctx context.Context,tx pgx.Tx,event outbox.Event) error {
    var domain outbox.DomainEvent
    if err:=json.Unmarshal(event.Payload,&domain); err!=nil {
        return p.quarantine(ctx,tx,event,"invalid_envelope_json")
    }
    if outbox.ValidateDomainEvent(domain)!=nil {
        return p.quarantine(ctx,tx,event,"invalid_domain_event")
    }
    if domain.Version!=1 {
        return p.quarantine(ctx,tx,event,"unsupported_schema_version")
    }
    if domain.Type!=event.EventType || domain.Aggregate.Type!=event.AggregateType || domain.Aggregate.ID!=event.AggregateID {
        return p.quarantine(ctx,tx,event,"outbox_envelope_mismatch")
    }
    allowed,ok:=eventDataFields[domain.Type]
    if !ok {
        return p.quarantine(ctx,tx,event,"unsupported_event_type")
    }
    var fields map[string]json.RawMessage
    if err:=json.Unmarshal(domain.Data,&fields); err!=nil || fields==nil {
        return p.quarantine(ctx,tx,event,"invalid_event_data")
    }
    for key:=range fields {
        if _,ok:=allowed[key]; !ok {
            return p.quarantine(ctx,tx,event,"unapproved_event_data_field")
        }
    }
    marketCode:=domain.Metadata["market_code"]
    if marketCode!="" && !marketCodePattern.MatchString(marketCode) {
        return p.quarantine(ctx,tx,event,"invalid_market_metadata")
    }
    trafficClass:=domain.Metadata["traffic_class"]
    if trafficClass!="" && trafficClass!="production" && trafficClass!="demo" && trafficClass!="test" {
        return p.quarantine(ctx,tx,event,"invalid_traffic_class")
    }

    digest:=sha256.Sum256(event.Payload)
    digestHex:=hex.EncodeToString(digest[:])
    tag,err:=tx.Exec(ctx,`INSERT INTO analytics_event_facts
        (tenant_id,event_id,outbox_event_id,event_type,event_version,occurred_at,aggregate_type,aggregate_id,market_code,traffic_class,data,payload_sha256)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),$11::jsonb,$12)
        ON CONFLICT DO NOTHING`,
        domain.TenantID,domain.ID,event.ID,domain.Type,domain.Version,domain.OccurredAt.UTC(),
        domain.Aggregate.Type,domain.Aggregate.ID,marketCode,trafficClass,[]byte(domain.Data),digestHex)
    if err!=nil { return err }
    if tag.RowsAffected()==0 {
        var existingHash string
        lookupErr:=tx.QueryRow(ctx,`SELECT payload_sha256 FROM analytics_event_facts WHERE tenant_id=$1 AND event_id=$2`,domain.TenantID,domain.ID).Scan(&existingHash)
        if errors.Is(lookupErr,pgx.ErrNoRows) {
            return p.quarantine(ctx,tx,event,"event_identity_conflict")
        }
        if lookupErr!=nil { return lookupErr }
        if existingHash!=digestHex {
            return p.quarantine(ctx,tx,event,"event_id_payload_conflict")
        }
    }
    return nil
}

func (p Projector) quarantine(ctx context.Context,tx pgx.Tx,event outbox.Event,reason string) error {
    digest:=sha256.Sum256(event.Payload)
    _,err:=tx.Exec(ctx,`INSERT INTO analytics_event_quarantine
        (outbox_event_id,event_type,aggregate_type,aggregate_id,reason_code,payload_sha256)
        VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(outbox_event_id) DO NOTHING`,
        event.ID,event.EventType,event.AggregateType,event.AggregateID,strings.TrimSpace(reason),hex.EncodeToString(digest[:]))
    return err
}
