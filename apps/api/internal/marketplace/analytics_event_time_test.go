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

	for _, name := range []string{"001_walking_skeleton.sql", "019_outbox_delivery.sql", "030_analytics_event_projection.sql", "034_analytics_event_time_policy.sql"} {
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

	lateEvent := appendEvent("late-"+suffix, time.Now().UTC().Add(-73*time.Hour))
	skewEvent := appendEvent("skew-"+suffix, time.Now().UTC().Add(time.Minute))
	liveProjector := analytics.Projector{DB:pool,ConsumerName:consumerName,GenerationID:generation,TenantID:tenant}
	liveConsumer := liveProjector.Consumer()

	// Deliberately process in the reverse of source insertion order.
	if applied, processErr := liveConsumer.Process(ctx, skewEvent); processErr != nil || !applied { t.Fatalf("process clock-skew event: applied=%v err=%v", applied, processErr) }
	if applied, processErr := liveConsumer.Process(ctx, lateEvent); processErr != nil || !applied { t.Fatalf("process late event: applied=%v err=%v", applied, processErr) }
	if applied, processErr := liveConsumer.Process(ctx, lateEvent); processErr != nil || applied { t.Fatalf("duplicate delivery must be a no-op: applied=%v err=%v", applied, processErr) }

	var occurredAt, ingestedAt time.Time
	var late, included bool
	if err = pool.QueryRow(ctx, `SELECT occurred_at,ingested_at,late_arrival,included_in_metrics
		FROM analytics_event_facts WHERE tenant_id=$1 AND generation_id=$2 AND event_id=$3`,
		tenant,generation,"late-"+suffix).Scan(&occurredAt,&ingestedAt,&late,&included); err != nil { t.Fatal(err) }
	if !occurredAt.Equal(time.Date(2026, occurredAt.Month(), occurredAt.Day(), occurredAt.Hour(), occurredAt.Minute(), occurredAt.Second(), occurredAt.Nanosecond(), time.UTC)) || !late || included {
		t.Fatalf("late fact must preserve source time and remain excluded pending correction: occurred=%s ingested=%s late=%v included=%v", occurredAt, ingestedAt, late, included)
	}
	var skewQuarantine int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM analytics_event_quarantine WHERE tenant_id=$1 AND generation_id=$2 AND outbox_event_id=$3 AND reason_code='occurred_at_after_ingestion_time'", tenant,generation,skewEvent.ID).Scan(&skewQuarantine); err != nil { t.Fatal(err) }
	if skewQuarantine != 1 { t.Fatalf("future source timestamp must be quarantined, got %d", skewQuarantine) }

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
