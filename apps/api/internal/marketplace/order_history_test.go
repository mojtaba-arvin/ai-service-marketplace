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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mojtaba-arvin/ai-service-marketplace/apps/api/internal/outbox"
)

func TestAuthorizedPaginatedOrderHistoryPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","004_catalog.sql","005_pricing.sql","007_market_config.sql","008_catalog_localization.sql","015_order_schedule.sql","022_identity_memberships.sql","023_order_tenant_scope.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=execMigrationScript(ctx, pool, migration); err!=nil { t.Fatalf("apply %s: %v",name,err) }
	}
	store:=&Store{DB:pool}
	ids:=make([]string,2)
	for i:=range ids {
		ids[i],err=newID()
		if err!=nil { t.Fatal(err) }
		phone:=fmt.Sprintf("+1992%011d",(time.Now().UnixNano()+int64(i))%100_000_000_000)
		if _,err=pool.Exec(ctx,`INSERT INTO phone_identities(id,phone_e164) VALUES($1,$2)`,ids[i],phone); err!=nil { t.Fatal(err) }
	}
	customer:=Actor{SubjectID:ids[0],TenantID:"market-history",Role:RoleCustomer}
	other:=Actor{SubjectID:ids[1],TenantID:"market-history",Role:RoleCustomer}
	if _,err=pool.Exec(ctx,`INSERT INTO markets(code,default_locale,currency,timezone,policy_version,enabled) VALUES('OM','en','OMR','Asia/Muscat','history-v1',true) ON CONFLICT(code) DO UPDATE SET enabled=true`);err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO market_locales(market_code,locale,is_default) VALUES('OM','en',true) ON CONFLICT(market_code,locale) DO NOTHING`);err!=nil { t.Fatal(err) }
	category,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("history-%d",time.Now().UnixNano()),"History")
	if err!=nil { t.Fatal(err) }
	if _,err=pool.Exec(ctx,`INSERT INTO market_category_visibility(market_code,category_id,visible) VALUES('OM',$1,true)`,category.ID);err!=nil { t.Fatal(err) }
	menu,err:=store.CreatePriceMenu(ctx,category.ID,"history-service")
	if err!=nil { t.Fatal(err) }
	if _,err=store.CreatePriceVersion(ctx,menu.ID,2100,"OMR"); err!=nil { t.Fatal(err) }
	create:=func(actor Actor,key string) Order {
		order,created,createErr:=store.CreateScheduledOrder(ctx,actor,key,menu.ID,time.Now().UTC().Add(2*time.Hour),
			"OM","Muscat","Muscat","Al-Khuwair")
		if createErr!=nil || !created { t.Fatalf("create order: %#v created=%v err=%v",order,created,createErr) }
		return order
	}
	first:=create(customer,"history-first-"+fmt.Sprint(time.Now().UnixNano()))
	second:=create(customer,"history-second-"+fmt.Sprint(time.Now().UnixNano()))
	third:=create(customer,"history-third-"+fmt.Sprint(time.Now().UnixNano()))
	otherOrder:=create(other,"history-other-"+fmt.Sprint(time.Now().UnixNano()))
	// The same identity may have memberships in multiple tenants. Its order
	// history and timeline must remain scoped to the active tenant context.
	secondTenant := Actor{SubjectID:customer.SubjectID,TenantID:"market-history-secondary",Role:RoleCustomer}
	secondTenantOrder := create(secondTenant,"history-secondary-"+fmt.Sprint(time.Now().UnixNano()))
	if _,err=store.TransitionOrder(ctx,first.ID,"accepted"); err!=nil { t.Fatal(err) }

	page1,err:=store.ListOrderHistory(ctx,customer,2,nil)
	if err!=nil { t.Fatal(err) }
	if len(page1.Items)!=2 || !page1.HasMore || page1.NextCursor==nil {
		t.Fatalf("first page should be paginated: %#v",page1)
	}
	page2,err:=store.ListOrderHistory(ctx,customer,2,page1.NextCursor)
	if err!=nil { t.Fatal(err) }
	if len(page2.Items)!=1 || page2.HasMore { t.Fatalf("second page should contain remaining customer order: %#v",page2) }
	seen:=map[string]bool{}
	for _,item:=range append(page1.Items,page2.Items...) { seen[item.Order.ID]=true }
	if !seen[first.ID] || !seen[second.ID] || !seen[third.ID] || seen[otherOrder.ID] || seen[secondTenantOrder.ID] {
		t.Fatalf("history leaked or omitted orders: %#v",seen)
	}
	timeline,err:=store.GetOrderTimeline(ctx,customer,first.ID)
	if err!=nil { t.Fatal(err) }
	types:=map[string]bool{}
	for _,event:=range timeline.Events { types[event.Type]=true }
	if !types["order.created"] || !types["order.state_changed"] {
		t.Fatalf("timeline missing order lifecycle events: %#v",timeline.Events)
	}
	var createdEvent *OrderHistoryEvent
	for i:=range timeline.Events { if timeline.Events[i].Type=="order.created" { createdEvent=&timeline.Events[i]; break } }
	if createdEvent==nil || len(createdEvent.DomainEvent)==0 { t.Fatalf("canonical envelope must be exposed separately: %#v",timeline.Events) }
	var legacyPayload map[string]any
	if err=json.Unmarshal(createdEvent.Payload,&legacyPayload); err!=nil { t.Fatal(err) }
	if legacyPayload["order_id"]!=first.ID || legacyPayload["status"]!="requested" { t.Fatalf("timeline business payload shape changed: %#v",legacyPayload) }

	if _,err=store.GetOrderTimeline(ctx,other,first.ID); !errors.Is(err,ErrOrderNotFound) {
		t.Fatalf("another customer cannot read order timeline: %v",err)
	}
	if _,err=store.GetOrderTimeline(ctx,customer,secondTenantOrder.ID); !errors.Is(err,ErrOrderNotFound) {
		t.Fatalf("same identity in another tenant cannot read timeline: %v",err)
	}
	if _,err=store.TransitionOrderAsActor(ctx,customer,secondTenantOrder.ID,"cancelled"); !errors.Is(err,ErrForbidden) {
		t.Fatalf("same identity in another tenant cannot transition order: %v",err)
	}
	otherPage,err:=store.ListOrderHistory(ctx,other,20,nil)
	if err!=nil || len(otherPage.Items)!=1 || otherPage.Items[0].Order.ID!=otherOrder.ID {
		t.Fatalf("other customer must see only own order: %#v %v",otherPage,err)
	}
	if _,err=store.ListOrderHistory(ctx,Actor{SubjectID:customer.SubjectID,TenantID:customer.TenantID,Role:RoleSupport},20,nil); !errors.Is(err,ErrForbidden) {
		t.Fatalf("support role cannot enumerate customer history: %v",err)
	}
	if _,err=store.ListOrderHistory(ctx,customer,20,&OrderHistoryCursor{}); !errors.Is(err,ErrInvalidHistoryCursor) {
		t.Fatalf("malformed cursor must fail: %v",err)
	}
}

func TestNormalizeOrderHistoryEventCompatibility(t *testing.T) {
	const aggregateType, aggregateID, tenantID := "order", "order-1", "tenant-1"

	for _, raw := range []string{
		`{"from":"requested","to":"accepted"}`,
		`{"version":2,"status":"legacy"}`,
		`{"version":2,"data":{"legacy_marker":true}}`,
	} {
		legacy := OrderHistoryEvent{Type:"order.state_changed",Payload:json.RawMessage(raw)}
		original := string(legacy.Payload)
		if err:=normalizeOrderHistoryEvent(&legacy,aggregateType,aggregateID,tenantID);err!=nil {
			t.Fatalf("legacy payload %s must remain consumable: %v",raw,err)
		}
		if string(legacy.Payload)!=original || len(legacy.DomainEvent)!=0 {
			t.Fatalf("legacy payload must remain byte-for-byte unchanged: %#v",legacy)
		}
	}

	base := outbox.DomainEvent{
		ID:"evt-1",Type:"order.created",Version:1,
		OccurredAt:time.Date(2026,11,22,12,0,0,0,time.UTC),TenantID:tenantID,
		Aggregate:outbox.EventAggregate{Type:aggregateType,ID:aggregateID},
		Data:json.RawMessage(`{"order_id":"order-1","status":"requested"}`),
	}
	encoded,err:=json.Marshal(base)
	if err!=nil { t.Fatal(err) }
	canonical:=OrderHistoryEvent{Type:"order.created",Payload:encoded}
	if err=normalizeOrderHistoryEvent(&canonical,aggregateType,aggregateID,tenantID);err!=nil {
		t.Fatalf("canonical v1 event should be accepted: %v",err)
	}
	if string(canonical.Payload)!=string(base.Data)||string(canonical.DomainEvent)!=string(encoded) {
		t.Fatalf("canonical event should expose both compatible payload and envelope: %#v",canonical)
	}

	malformed:=json.RawMessage(`{"version":1,"tenant_id":"tenant-1","aggregate":{"type":"order","id":"order-1"},"data":{}}`)
	if err=normalizeOrderHistoryEvent(&OrderHistoryEvent{Type:"order.created",Payload:malformed},aggregateType,aggregateID,tenantID);!errors.Is(err,outbox.ErrInvalidPolicy) {
		t.Fatalf("malformed envelope must fail closed, got %v",err)
	}

	cases:=[]struct{
		name string
		mutate func(*outbox.DomainEvent)
		eventType string
	}{
		{name:"wrong aggregate type",mutate:func(e *outbox.DomainEvent){e.Aggregate.Type="booking"}},
		{name:"wrong aggregate id",mutate:func(e *outbox.DomainEvent){e.Aggregate.ID="another-order"}},
		{name:"wrong tenant",mutate:func(e *outbox.DomainEvent){e.TenantID="another-tenant"}},
		{name:"unsupported schema version",mutate:func(e *outbox.DomainEvent){e.Version=2}},
		{name:"event type mismatch",eventType:"order.cancelled"},
	}
	for _,tc:=range cases {
		t.Run(tc.name,func(t *testing.T){
			event:=base
			if tc.mutate!=nil { tc.mutate(&event) }
			raw,marshalErr:=json.Marshal(event)
			if marshalErr!=nil { t.Fatal(marshalErr) }
			typ:=event.Type
			if tc.eventType!="" { typ=tc.eventType }
			err:=normalizeOrderHistoryEvent(&OrderHistoryEvent{Type:typ,Payload:raw},aggregateType,aggregateID,tenantID)
			if !errors.Is(err,outbox.ErrInvalidPolicy) {
				t.Fatalf("incompatible canonical event must fail closed, got %v",err)
			}
		})
	}
}
