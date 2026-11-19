package marketplace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthorizedPaginatedOrderHistoryPostgres(t *testing.T) {
	dsn:=os.Getenv("TEST_DATABASE_URL")
	if dsn=="" { t.Skip("TEST_DATABASE_URL is not configured") }
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	pool,err:=pgxpool.New(ctx,dsn)
	if err!=nil { t.Fatal(err) }
	defer pool.Close()
	for _,name:=range []string{"001_walking_skeleton.sql","002_identity.sql","004_catalog.sql","005_pricing.sql","015_order_schedule.sql"} {
		migration,readErr:=os.ReadFile(filepath.Join("..","..","migrations",name))
		if readErr!=nil { t.Fatal(readErr) }
		if _,err=pool.Exec(ctx,string(migration)); err!=nil { t.Fatalf("apply %s: %v",name,err) }
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
	category,err:=store.CreateCategory(ctx,nil,fmt.Sprintf("history-%d",time.Now().UnixNano()),"History")
	if err!=nil { t.Fatal(err) }
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
	if !seen[first.ID] || !seen[second.ID] || !seen[third.ID] || seen[otherOrder.ID] {
		t.Fatalf("history leaked or omitted orders: %#v",seen)
	}
	timeline,err:=store.GetOrderTimeline(ctx,customer,first.ID)
	if err!=nil { t.Fatal(err) }
	types:=map[string]bool{}
	for _,event:=range timeline.Events { types[event.Type]=true }
	if !types["order.created"] || !types["order.state_changed"] {
		t.Fatalf("timeline missing order lifecycle events: %#v",timeline.Events)
	}
	if _,err=store.GetOrderTimeline(ctx,other,first.ID); !errors.Is(err,ErrForbidden) {
		t.Fatalf("another customer cannot read order timeline: %v",err)
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
