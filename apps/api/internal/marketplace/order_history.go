package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInvalidHistoryCursor = errors.New("invalid order history cursor")

type OrderHistoryCursor struct {
	CreatedAt time.Time `json:"created_at"`
	OrderID string `json:"order_id"`
}

type OrderHistoryEvent struct {
	ID int64 `json:"id"`
	Type string `json:"type"`
	Payload json.RawMessage `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

type OrderHistoryItem struct {
	Order Order `json:"order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Events []OrderHistoryEvent `json:"events"`
}

type OrderHistoryPage struct {
	Items []OrderHistoryItem `json:"items"`
	NextCursor *OrderHistoryCursor `json:"next_cursor,omitempty"`
	HasMore bool `json:"has_more"`
}

func authorizeOrderHistory(actor Actor) error {
	if actor.Role!=RoleCustomer || actor.SubjectID=="" || actor.TenantID=="" { return ErrForbidden }
	return Authorize(actor,Resource{TenantID:actor.TenantID,OwnerID:actor.SubjectID},ActionOrderRead)
}

func loadOrderHistoryEvents(ctx context.Context,tx pgx.Tx,orderID string) ([]OrderHistoryEvent,error) {
	rows,err:=tx.Query(ctx,`
		SELECT id,event_type,payload,created_at FROM outbox_events
		WHERE aggregate_type='order' AND aggregate_id=$1 ORDER BY id
	`,orderID)
	if err!=nil { return nil,err }
	defer rows.Close()
	events:=make([]OrderHistoryEvent,0)
	for rows.Next() {
		var event OrderHistoryEvent
		if err=rows.Scan(&event.ID,&event.Type,&event.Payload,&event.CreatedAt); err!=nil { return nil,err }
		events=append(events,event)
	}
	if err=rows.Err(); err!=nil { return nil,err }
	return events,nil
}

func scanOrderHistoryItem(row pgx.Row) (OrderHistoryItem,error) {
	var item OrderHistoryItem
	err:=row.Scan(&item.Order.ID,&item.Order.CustomerID,&item.Order.AmountMinor,&item.Order.Currency,
		&item.Order.Status,&item.Order.PriceVersionID,&item.Order.ScheduledAt,&item.Order.CountryCode,
		&item.Order.RegionCode,&item.Order.CityCode,&item.Order.AreaCode,&item.CreatedAt,&item.UpdatedAt)
	return item,err
}

func (s *Store) ListOrderHistory(ctx context.Context,actor Actor,limit int,cursor *OrderHistoryCursor) (OrderHistoryPage,error) {
	if err:=authorizeOrderHistory(actor); err!=nil { return OrderHistoryPage{},err }
	if limit<1 { limit=20 }
	if limit>50 { limit=50 }
	if cursor!=nil && (cursor.CreatedAt.IsZero() || cursor.OrderID=="") { return OrderHistoryPage{},ErrInvalidHistoryCursor }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{AccessMode:pgx.ReadOnly})
	if err!=nil { return OrderHistoryPage{},err }
	defer tx.Rollback(ctx)
	var rows pgx.Rows
	if cursor==nil {
		rows,err=tx.Query(ctx,`
			SELECT id,customer_id,amount_minor,currency,status,price_version_id,scheduled_at,country_code,region_code,city_code,area_code,created_at,updated_at
			FROM orders WHERE customer_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2
		`,actor.SubjectID,limit+1)
	} else {
		rows,err=tx.Query(ctx,`
			SELECT id,customer_id,amount_minor,currency,status,price_version_id,scheduled_at,country_code,region_code,city_code,area_code,created_at,updated_at
			FROM orders WHERE customer_id=$1 AND (created_at,id)<($2,$3)
			ORDER BY created_at DESC,id DESC LIMIT $4
		`,actor.SubjectID,cursor.CreatedAt,cursor.OrderID,limit+1)
	}
	if err!=nil { return OrderHistoryPage{},err }
	items:=make([]OrderHistoryItem,0,limit+1)
	for rows.Next() {
		item,scanErr:=scanOrderHistoryItem(rows)
		if scanErr!=nil { rows.Close(); return OrderHistoryPage{},scanErr }
		items=append(items,item)
	}
	if err=rows.Err(); err!=nil { rows.Close(); return OrderHistoryPage{},err }
	rows.Close()
	page:=OrderHistoryPage{Items:items}
	if len(items)>limit {
		page.HasMore=true
		page.Items=items[:limit]
		last:=page.Items[len(page.Items)-1]
		page.NextCursor=&OrderHistoryCursor{CreatedAt:last.CreatedAt,OrderID:last.Order.ID}
	}
	for i:=range page.Items {
		page.Items[i].Events,err=loadOrderHistoryEvents(ctx,tx,page.Items[i].Order.ID)
		if err!=nil { return OrderHistoryPage{},err }
	}
	if err=tx.Commit(ctx); err!=nil { return OrderHistoryPage{},err }
	return page,nil
}

func (s *Store) GetOrderTimeline(ctx context.Context,actor Actor,orderID string) (OrderHistoryItem,error) {
	if err:=authorizeOrderHistory(actor); err!=nil { return OrderHistoryItem{},err }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{AccessMode:pgx.ReadOnly})
	if err!=nil { return OrderHistoryItem{},err }
	defer tx.Rollback(ctx)
	item,err:=scanOrderHistoryItem(tx.QueryRow(ctx,`
		SELECT id,customer_id,amount_minor,currency,status,price_version_id,scheduled_at,country_code,region_code,city_code,area_code,created_at,updated_at
		FROM orders WHERE id=$1
	`,orderID))
	if errors.Is(err,pgx.ErrNoRows) { return OrderHistoryItem{},ErrOrderNotFound }
	if err!=nil { return OrderHistoryItem{},err }
	if item.Order.CustomerID!=actor.SubjectID { return OrderHistoryItem{},ErrForbidden }
	item.Events,err=loadOrderHistoryEvents(ctx,tx,orderID)
	if err!=nil { return OrderHistoryItem{},err }
	if err=tx.Commit(ctx); err!=nil { return OrderHistoryItem{},err }
	return item,nil
}
