package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

type Projector struct {
	DB *pgxpool.Pool
	ConsumerName string
	GenerationID string
	TenantID string
	// IncludeLateEvents is only for an explicitly authorized correction
	// generation. Live workers must leave it false.
	IncludeLateEvents bool
}

var marketCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)

var requiredEventDataFields = map[string][]string{
	"order.created":{"order_id","status","price_version_id","scheduled_at"},
	"order.state_changed":{"order_id","from","to"},
	"order.provider_assigned":{"order_id","provider_id","assignment_id","offer_id"},
	"order.cancelled":{"order_id","from","to","cancellation_id"},
	"provider.booking_reserved":{"booking_id","provider_id","order_id","start_at","end_at"},
	"provider.booking_cancelled":{"order_id","cancellation_id","status"},
	"dispatch.offer_created":{"offer_id","order_id","provider_id","expires_at"},
	"dispatch.offer_declined":{"offer_id","order_id","provider_id"},
	"dispatch.offer_expired":{"offer_id","order_id"},
}

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
	if strings.TrimSpace(p.GenerationID)=="" { return errors.New("analytics generation ID is required") }
	var domain outbox.DomainEvent
	if err:=json.Unmarshal(event.Payload,&domain); err!=nil {
		return p.quarantine(ctx,tx,event,"invalid_envelope_json")
	}
	if strings.TrimSpace(p.TenantID)!="" && strings.TrimSpace(domain.TenantID)!="" && strings.TrimSpace(domain.TenantID)!=strings.TrimSpace(p.TenantID) {
		return errors.New("analytics event tenant does not match projection tenant")
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
	policy,ok:=EventTimePolicyFor(domain.Type)
	if !ok {
		return p.quarantine(ctx,tx,event,"unsupported_metric_time_policy")
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
	for _,key:=range requiredEventDataFields[domain.Type] {
		raw,ok:=fields[key]
		var value string
		if !ok || json.Unmarshal(raw,&value)!=nil || strings.TrimSpace(value)=="" {
			return p.quarantine(ctx,tx,event,"missing_required_event_data")
		}
	}
	if domain.Type=="dispatch.offer_created" {
		rawExpiry:=fields["expires_at"]
		var expiresAt time.Time
		if json.Unmarshal(rawExpiry,&expiresAt)!=nil || !expiresAt.After(domain.OccurredAt) {
			return p.quarantine(ctx,tx,event,"invalid_offer_expiry")
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

	// Read the durable source ingestion timestamp from the outbox row. Never
	// substitute projected_at or an outbox sequence ID for this timestamp.
	var ingestedAt time.Time
	var ingestionTimeSource string
	if err:=tx.QueryRow(ctx,`SELECT created_at,ingested_at_source FROM outbox_events WHERE id=$1 AND payload->>'tenant_id'=$2`,
		event.ID,domain.TenantID).Scan(&ingestedAt,&ingestionTimeSource); err!=nil {
		return err
	}
	if ingestionTimeSource!="insert_clock" {
		return p.quarantine(ctx,tx,event,"ingestion_time_untrusted")
	}
	classification,err:=ClassifyEventTime(domain.OccurredAt,ingestedAt,policy)
	if err!=nil {
		return p.quarantine(ctx,tx,event,"invalid_event_time")
	}
	if classification.ClockSkew {
		return p.quarantine(ctx,tx,event,"occurred_at_after_ingestion_time")
	}
	includedInMetrics:=!classification.Late || p.IncludeLateEvents

	digest:=sha256.Sum256(event.Payload)
	digestHex:=hex.EncodeToString(digest[:])
	tag,err:=tx.Exec(ctx,`INSERT INTO analytics_event_facts
		(tenant_id,generation_id,event_id,outbox_event_id,event_type,event_version,occurred_at,ingested_at,
		 event_time_family,lateness_policy_version,allowed_lateness_seconds,late_arrival,included_in_metrics,ingestion_time_trusted,
		 aggregate_type,aggregate_id,market_code,traffic_class,data,payload_sha256)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NULLIF($17,''),NULLIF($18,''),$19::jsonb,$20)
		ON CONFLICT DO NOTHING`,
		domain.TenantID,p.GenerationID,domain.ID,event.ID,domain.Type,domain.Version,domain.OccurredAt.UTC(),ingestedAt.UTC(),
		policy.MetricFamily,policy.Version,int64(policy.AllowedLateness.Seconds()),classification.Late,includedInMetrics,true,
		domain.Aggregate.Type,domain.Aggregate.ID,marketCode,trafficClass,[]byte(domain.Data),digestHex)
	if err!=nil { return err }
	if tag.RowsAffected()==0 {
		var existingHash string
		var canonicalOutboxID int64
		lookupErr:=tx.QueryRow(ctx,`SELECT payload_sha256,outbox_event_id FROM analytics_event_facts
			WHERE tenant_id=$1 AND generation_id=$2 AND event_id=$3`,
			domain.TenantID,p.GenerationID,domain.ID).Scan(&existingHash,&canonicalOutboxID)
		if errors.Is(lookupErr,pgx.ErrNoRows) {
			return p.quarantine(ctx,tx,event,"event_identity_conflict")
		}
		if lookupErr!=nil { return lookupErr }
		if existingHash!=digestHex {
			return p.quarantine(ctx,tx,event,"event_id_payload_conflict")
		}
		if canonicalOutboxID!=event.ID {
			_,err=tx.Exec(ctx,`INSERT INTO analytics_event_duplicates
				(tenant_id,generation_id,outbox_event_id,canonical_outbox_event_id,event_id,event_version,
				 occurred_at,ingested_at,market_code,event_time_family,ingestion_time_source,lateness_policy_version,allowed_lateness_seconds,late_arrival,payload_sha256)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11,$12,$13,$14,$15)
				ON CONFLICT(tenant_id,generation_id,outbox_event_id) DO NOTHING`,
				domain.TenantID,p.GenerationID,event.ID,canonicalOutboxID,domain.ID,domain.Version,
				domain.OccurredAt.UTC(),ingestedAt.UTC(),marketCode,policy.MetricFamily,ingestionTimeSource,policy.Version,int64(policy.AllowedLateness.Seconds()),classification.Late,digestHex)
			if err!=nil { return err }
		}
	}
	return nil
}

func (p Projector) quarantine(ctx context.Context,tx pgx.Tx,event outbox.Event,reason string) error {
	digest:=sha256.Sum256(event.Payload)
	var domain outbox.DomainEvent
	_ = json.Unmarshal(event.Payload,&domain)
	var eventID, eventVersion, occurredAt, marketCode, family any
	if strings.TrimSpace(domain.ID)!="" { eventID=domain.ID }
	if domain.Version>0 { eventVersion=domain.Version }
	if !domain.OccurredAt.IsZero() { occurredAt=domain.OccurredAt.UTC() }
	if marketCodeValue:=domain.Metadata["market_code"]; marketCodePattern.MatchString(marketCodeValue) { marketCode=marketCodeValue }
	if policy,ok:=EventTimePolicyFor(domain.Type); ok { family=policy.MetricFamily }
	var ingestedAt time.Time
	var ingestionTimeSource string
	if err:=tx.QueryRow(ctx,`SELECT created_at,ingested_at_source FROM outbox_events WHERE id=$1`,event.ID).Scan(&ingestedAt,&ingestionTimeSource); err!=nil { return err }
	_,err:=tx.Exec(ctx,`INSERT INTO analytics_event_quarantine
		(tenant_id,generation_id,outbox_event_id,event_id,event_version,occurred_at,ingested_at,market_code,event_time_family,
		 ingestion_time_source,ingestion_time_trusted,event_type,aggregate_type,aggregate_id,reason_code,payload_sha256)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT(tenant_id,generation_id,outbox_event_id) DO NOTHING`,
		p.tenantID(event),p.GenerationID,event.ID,eventID,eventVersion,occurredAt,ingestedAt.UTC(),marketCode,family,
		ingestionTimeSource,ingestionTimeSource=="insert_clock",event.EventType,event.AggregateType,event.AggregateID,strings.TrimSpace(reason),hex.EncodeToString(digest[:]))
	return err
}

func (p Projector) tenantID(event outbox.Event) string {
	if strings.TrimSpace(p.TenantID)!="" { return strings.TrimSpace(p.TenantID) }
	var envelope outbox.DomainEvent
	if json.Unmarshal(event.Payload,&envelope)==nil { return strings.TrimSpace(envelope.TenantID) }
	return ""
}
