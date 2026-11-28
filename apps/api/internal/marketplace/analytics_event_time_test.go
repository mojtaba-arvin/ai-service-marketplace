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
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/analytics"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func TestAnalyticsEventTimeLateClockSkewAndCorrectionReplayPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil { t.Fatal(err) }
	defer pool.Close()

	for _, name := range []string{"001_walking_skeleton.sql", "019_outbox_delivery.sql", "030_analytics_event_projection.sql", "036_analytics_event_time_policy.sql"} {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if readErr != nil { t.Fatal(readErr) }
		if _, err = execMigrationScript(ctx, pool, raw); err != nil { t.Fatalf("apply %s: %v", name, err) }
	}

	suffix := fmt.Sprint(time.Now().UnixNano())
	tenant := "analytics-event-time-" + suffix
	generation := "event-time-live-" + suffix
	consumerName := "analytics:" + tenant + ":" + generation
	if _, err = pool.Exec(ctx, "INSERT INTO analytics_projection_generations(tenant_id,generation_id,consumer_name,status,activated_at) VALUES($1,$2,$3,'active',now())", tenant, generation, consumerName); err != nil { t.Fatal(err) }
	if _, err = pool.Exec(ctx, "INSERT INTO analytics_active_generations(tenant_id,generation_id) VALUES($1,$2)", tenant, generation); err != nil { t.Fatal(err) }

	appendEvent := func(id string, occurred time.Time) outbox.Event {
		t.Helper()
		orderID := "order-" + id
		data, marshalErr := json.Marshal(map[string]string{"order_id":orderID,"status":"requested","price_version_id":"price-v1","scheduled_at":"2026-11-29T10:00:00Z"})
		if marshalErr != nil { t.Fatal(marshalErr) }
		domain := outbox.DomainEvent{
			ID:id, Type:"order.created", Version:1, OccurredAt:occurred.UTC(), TenantID:tenant,
			Aggregate:outbox.EventAggregate{Type:"order",ID:orderID},
			Metadata:map[string]string{"market_code":"OM","traffic_class":"production"}, Data:data,
		}
		tx, beginErr := pool.BeginTx(ctx, pgx.TxOptions{})
		if beginErr != nil { t.Fatal(beginErr) }
		outboxID, appendErr := outbox.AppendDomainEvent(ctx, tx, domain)
		if appendErr != nil { _ = tx.Rollback(ctx); t.Fatal(appendErr) }
		if commitErr := tx.Commit(ctx); commitErr != nil { t.Fatal(commitErr) }
		var event outbox.Event
		if scanErr := pool.QueryRow(ctx, "SELECT id,aggregate_type,aggregate_id,event_type,payload,attempts FROM outbox_events WHERE id=$1", outboxID).
			Scan(&event.ID,&event.AggregateType,&event.AggregateID,&event.EventType,&event.Payload,&event.Attempts); scanErr != nil { t.Fatal(scanErr) }
		return event
	}

	lateOccurredAt := time.Now().UTC().Add(-73*time.Hour).Truncate(time.Microsecond)
	lateEvent := appendEvent("late-"+suffix, lateOccurredAt)
	duplicateEvent := appendEvent("late-"+suffix, lateOccurredAt)
	conflictingEvent := appendEvent("late-"+suffix, lateOccurredAt.Add(time.Second))
	reorderedLowID := appendEvent("reordered-"+suffix, lateOccurredAt)
	reorderedHighID := appendEvent("reordered-"+suffix, lateOccurredAt)
	skewEvent := appendEvent("skew-"+suffix, time.Now().UTC().Add(time.Minute))
	legacyIngestionEvent := appendEvent("legacy-ingestion-"+suffix, time.Now().UTC())
	if _, err = pool.Exec(ctx, "UPDATE outbox_events SET ingested_at_source='legacy_transaction_timestamp' WHERE id=$1", legacyIngestionEvent.ID); err != nil { t.Fatal(err) }
	liveProjector := analytics.Projector{DB:pool,ConsumerName:consumerName,GenerationID:generation,TenantID:tenant}
	liveConsumer := liveProjector.Consumer()

	// Deliberately process in the reverse of source insertion order.
	if applied, processErr := liveConsumer.Process(ctx, skewEvent); processErr != nil || !applied { t.Fatalf("process clock-skew event: applied=%v err=%v", applied, processErr) }
	if applied, processErr := liveConsumer.Process(ctx, legacyIngestionEvent); processErr != nil || !applied { t.Fatalf("process legacy-ingestion event: applied=%v err=%v", applied, processErr) }
	if applied, processErr := liveConsumer.Process(ctx, lateEvent); processErr != nil || !applied { t.Fatalf("process late event: applied=%v err=%v", applied, processErr) }
	if applied, processErr := liveConsumer.Process(ctx, duplicateEvent); processErr != nil || !applied { t.Fatalf("process duplicate logical event: applied=%v err=%v", applied, processErr) }
	if applied, processErr := liveConsumer.Process(ctx, conflictingEvent); processErr != nil || !applied { t.Fatalf("process conflicting event ID: applied=%v err=%v", applied, processErr) }
	// Deliver the higher outbox ID first; the lower-ID duplicate later reveals
	// that deterministic replay will choose a different canonical source row.
	if applied, processErr := liveConsumer.Process(ctx, reorderedHighID); processErr != nil || !applied { t.Fatalf("process higher-ID duplicate first: applied=%v err=%v", applied, processErr) }
	if applied, processErr := liveConsumer.Process(ctx, reorderedLowID); processErr != nil || !applied { t.Fatalf("process lower-ID duplicate second: applied=%v err=%v", applied, processErr) }
	if applied, processErr := liveConsumer.Process(ctx, lateEvent); processErr != nil || applied { t.Fatalf("same outbox delivery must be a no-op: applied=%v err=%v", applied, processErr) }
	var duplicateCount int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM analytics_event_duplicates WHERE tenant_id=$1 AND generation_id=$2 AND outbox_event_id=$3", tenant,generation,duplicateEvent.ID).Scan(&duplicateCount); err != nil { t.Fatal(err) }
	if duplicateCount != 1 { t.Fatalf("identical logical event must have one duplicate outcome, got %d", duplicateCount) }
	var reorderConflictCount int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM analytics_event_duplicates WHERE tenant_id=$1 AND generation_id=$2 AND outbox_event_id=$3 AND outbox_event_id < canonical_outbox_event_id", tenant,generation,reorderedLowID.ID).Scan(&reorderConflictCount); err != nil { t.Fatal(err) }
	if reorderConflictCount != 1 { t.Fatalf("out-of-order lower-ID duplicate must request deterministic correction, got %d", reorderConflictCount) }
	var conflictCount int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM analytics_event_quarantine WHERE tenant_id=$1 AND generation_id=$2 AND outbox_event_id=$3 AND reason_code='event_id_payload_conflict'", tenant,generation,conflictingEvent.ID).Scan(&conflictCount); err != nil { t.Fatal(err) }
	if conflictCount != 1 { t.Fatalf("conflicting payload for same event ID must quarantine, got %d", conflictCount) }
	var legacyReason string
	var legacyTrusted bool
	if err = pool.QueryRow(ctx, `SELECT reason_code,ingestion_time_trusted FROM analytics_event_quarantine
		WHERE tenant_id=$1 AND generation_id=$2 AND outbox_event_id=$3`,tenant,generation,legacyIngestionEvent.ID).Scan(&legacyReason,&legacyTrusted); err != nil { t.Fatal(err) }
	if legacyReason!="ingestion_time_untrusted" || legacyTrusted { t.Fatalf("legacy transaction-start timestamp must remain untrusted: reason=%q trusted=%v",legacyReason,legacyTrusted) }

	var occurredAt, ingestedAt time.Time
	var late, included, ingestionTrusted bool
	if err = pool.QueryRow(ctx, `SELECT occurred_at,ingested_at,late_arrival,included_in_metrics,ingestion_time_trusted
		FROM analytics_event_facts WHERE tenant_id=$1 AND generation_id=$2 AND event_id=$3`,
		tenant,generation,"late-"+suffix).Scan(&occurredAt,&ingestedAt,&late,&included,&ingestionTrusted); err != nil { t.Fatal(err) }
	if !occurredAt.Equal(lateOccurredAt) || !late || included || !ingestionTrusted || !ingestedAt.After(occurredAt) {
		t.Fatalf("late fact must preserve source time and remain excluded pending correction: occurred=%s ingested=%s late=%v included=%v", occurredAt, ingestedAt, late, included)
	}
	var skewQuarantine int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM analytics_event_quarantine WHERE tenant_id=$1 AND generation_id=$2 AND outbox_event_id=$3 AND reason_code='occurred_at_after_ingestion_time'", tenant,generation,skewEvent.ID).Scan(&skewQuarantine); err != nil { t.Fatal(err) }
	if skewQuarantine != 1 { t.Fatalf("future source timestamp must be quarantined, got %d", skewQuarantine) }
	var quarantinedEventID, quarantinedMarket, quarantinedIngestionSource string
	var quarantinedVersion int
	var quarantinedOccurred, quarantinedIngested time.Time
	var quarantinedIngestionTrusted bool
	if err = pool.QueryRow(ctx, `SELECT event_id,event_version,occurred_at,ingested_at,market_code,ingestion_time_source,ingestion_time_trusted
		FROM analytics_event_quarantine WHERE tenant_id=$1 AND generation_id=$2 AND outbox_event_id=$3`,
		tenant,generation,skewEvent.ID).Scan(&quarantinedEventID,&quarantinedVersion,&quarantinedOccurred,&quarantinedIngested,&quarantinedMarket,&quarantinedIngestionSource,&quarantinedIngestionTrusted); err != nil { t.Fatal(err) }
	if quarantinedEventID!="skew-"+suffix || quarantinedVersion!=1 || !quarantinedOccurred.After(quarantinedIngested) || quarantinedMarket!="OM" || quarantinedIngestionSource!="insert_clock" || !quarantinedIngestionTrusted {
		t.Fatalf("quarantine provenance incomplete: event=%q version=%d occurred=%s ingested=%s market=%q",quarantinedEventID,quarantinedVersion,quarantinedOccurred,quarantinedIngested,quarantinedMarket)
	}

	// A correction is an explicit new immutable generation. It includes the
	// same source event only because the correction flag is deliberately set.
	correctionGeneration := "event-time-correction-" + suffix
	correctionConsumerName := "analytics:" + tenant + ":" + correctionGeneration
	if _, err = pool.Exec(ctx, "INSERT INTO analytics_projection_generations(tenant_id,generation_id,consumer_name,status) VALUES($1,$2,$3,'building')", tenant,correctionGeneration,correctionConsumerName); err != nil { t.Fatal(err) }
	correctionProjector := analytics.Projector{DB:pool,ConsumerName:correctionConsumerName,GenerationID:correctionGeneration,TenantID:tenant,IncludeLateEvents:true}
	if applied, processErr := correctionProjector.Consumer().Process(ctx, lateEvent); processErr != nil || !applied { t.Fatalf("correction replay: applied=%v err=%v", applied, processErr) }
	var correctionIncluded bool
	var correctionOccurred, correctionIngested time.Time
	if err = pool.QueryRow(ctx, `SELECT occurred_at,ingested_at,included_in_metrics FROM analytics_event_facts
		WHERE tenant_id=$1 AND generation_id=$2 AND event_id=$3`, tenant,correctionGeneration,"late-"+suffix).
		Scan(&correctionOccurred,&correctionIngested,&correctionIncluded); err != nil { t.Fatal(err) }
	if !correctionIncluded || !correctionOccurred.Equal(occurredAt) || !correctionIngested.Equal(ingestedAt) {
		t.Fatalf("correction replay must preserve provenance and explicitly include the late fact: occurred=%s ingested=%s included=%v", correctionOccurred, correctionIngested, correctionIncluded)
	}
}
