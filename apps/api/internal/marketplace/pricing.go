package marketplace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrOrderIdempotencyConflict = errors.New("order idempotency key reused with different request")
	ErrPriceMenuNotFound = errors.New("price menu not found")
	ErrPriceUnavailable  = errors.New("price menu has no active version or its category is unavailable")
)

type PriceMenu struct {
	ID             string    `json:"id"`
	CategoryID     string    `json:"category_id"`
	ServiceCode    string    `json:"service_code"`
	ActiveVersionID *string  `json:"active_version_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PriceVersion struct {
	ID         string    `json:"id"`
	MenuID     string    `json:"menu_id"`
	VersionNo  int       `json:"version_no"`
	AmountMinor int64    `json:"amount_minor"`
	Currency   string    `json:"currency"`
	CreatedAt  time.Time `json:"created_at"`
}

func categoryAvailableForOrder(ctx context.Context, tx pgx.Tx, categoryID string) (bool, error) {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id FROM service_categories WHERE id=$1
			UNION ALL
			SELECT c.id,c.parent_id FROM service_categories c JOIN ancestors a ON c.id=a.parent_id
		)
		SELECT c.enabled
		FROM service_categories c JOIN ancestors a ON a.id=c.id
		ORDER BY c.id
		FOR SHARE OF c
	`, categoryID)
	if err != nil { return false, err }
	defer rows.Close()
	found, enabled := false, true
	for rows.Next() {
		var rowEnabled bool
		if err = rows.Scan(&rowEnabled); err != nil { return false, err }
		found = true
		enabled = enabled && rowEnabled
	}
	if err = rows.Err(); err != nil { return false, err }
	return found && enabled, nil
}

func (s *Store) CreatePriceMenu(ctx context.Context, categoryID, serviceCode string) (PriceMenu, error) {
	serviceCode = strings.ToLower(strings.TrimSpace(serviceCode))
	if categoryID == "" || !categorySlugPattern.MatchString(serviceCode) {
		return PriceMenu{}, ErrInvalidCategory
	}
	enabled, err := s.CanAcceptOrders(ctx, categoryID)
	if err != nil { return PriceMenu{}, err }
	if !enabled { return PriceMenu{}, ErrPriceUnavailable }
	id, err := newID()
	if err != nil { return PriceMenu{}, err }
	var menu PriceMenu
	err = s.DB.QueryRow(ctx, `
		INSERT INTO service_price_menus(id,category_id,service_code)
		VALUES($1,$2,$3)
		RETURNING id,category_id,service_code,active_version_id,created_at,updated_at
	`, id, categoryID, serviceCode).Scan(&menu.ID, &menu.CategoryID, &menu.ServiceCode,
		&menu.ActiveVersionID, &menu.CreatedAt, &menu.UpdatedAt)
	if err != nil { return PriceMenu{}, fmt.Errorf("create price menu: %w", err) }
	return menu, nil
}

func (s *Store) CreatePriceVersion(ctx context.Context, menuID string, amountMinor int64, currency string) (PriceVersion, error) {
	if err := ValidateAmount(amountMinor, currency); err != nil { return PriceVersion{}, err }
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return PriceVersion{}, err }
	defer tx.Rollback(ctx)

	var categoryID string
	err = tx.QueryRow(ctx, `SELECT category_id FROM service_price_menus WHERE id=$1 FOR UPDATE`, menuID).Scan(&categoryID)
	if errors.Is(err, pgx.ErrNoRows) { return PriceVersion{}, ErrPriceMenuNotFound }
	if err != nil { return PriceVersion{}, err }
	categoryEnabled, err := categoryAvailableForOrder(ctx, tx, categoryID)
	if err != nil { return PriceVersion{}, err }
	if !categoryEnabled { return PriceVersion{}, ErrPriceUnavailable }

	var versionNo int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_no),0)+1 FROM service_price_versions WHERE menu_id=$1`, menuID).Scan(&versionNo); err != nil {
		return PriceVersion{}, err
	}
	id, err := newID()
	if err != nil { return PriceVersion{}, err }
	var version PriceVersion
	err = tx.QueryRow(ctx, `
		INSERT INTO service_price_versions(id,menu_id,version_no,amount_minor,currency)
		VALUES($1,$2,$3,$4,$5)
		RETURNING id,menu_id,version_no,amount_minor,currency,created_at
	`, id, menuID, versionNo, amountMinor, currency).Scan(&version.ID, &version.MenuID,
		&version.VersionNo, &version.AmountMinor, &version.Currency, &version.CreatedAt)
	if err != nil { return PriceVersion{}, fmt.Errorf("create price version: %w", err) }
	if _, err = tx.Exec(ctx, `UPDATE service_price_menus SET active_version_id=$2,updated_at=now() WHERE id=$1`, menuID, id); err != nil {
		return PriceVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil { return PriceVersion{}, err }
	return version, nil
}

func (s *Store) GetCurrentPrice(ctx context.Context, menuID string) (PriceVersion, error) {
	var version PriceVersion
	err := s.DB.QueryRow(ctx, `
		SELECT v.id,v.menu_id,v.version_no,v.amount_minor,v.currency,v.created_at
		FROM service_price_menus m
		JOIN service_price_versions v ON v.id=m.active_version_id
		WHERE m.id=$1
	`, menuID).Scan(&version.ID, &version.MenuID, &version.VersionNo,
		&version.AmountMinor, &version.Currency, &version.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) { return PriceVersion{}, ErrPriceMenuNotFound }
	if err != nil { return PriceVersion{}, err }
	return version, nil
}

// CreateOrderFromPrice snapshots the active immutable price version in the same
// transaction as the order, ledger journal, and outbox event.
func (s *Store) CreateOrderFromPrice(ctx context.Context, customerID, idempotencyKey, menuID string) (Order, bool, error) {
	if customerID == "" || idempotencyKey == "" { return Order{}, false, errors.New("customer_id and Idempotency-Key are required") }
	if len(idempotencyKey) > 200 { return Order{}, false, errors.New("Idempotency-Key is too long") }
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return Order{}, false, err }
	defer tx.Rollback(ctx)

	// Idempotency takes precedence over current catalog availability: a replay
	// must return the original order snapshot even after the menu is repriced or disabled.
	var prior Order
	var priorMenuID string
	err = tx.QueryRow(ctx, `
		SELECT o.id,o.customer_id,o.amount_minor,o.currency,o.status,o.price_version_id,v.menu_id,
			o.scheduled_at,o.country_code,o.region_code,o.city_code,o.area_code
		FROM orders o JOIN service_price_versions v ON v.id=o.price_version_id
		WHERE o.customer_id=$1 AND o.idempotency_key=$2
	`, customerID, idempotencyKey).Scan(&prior.ID, &prior.CustomerID, &prior.AmountMinor,
		&prior.Currency, &prior.Status, &prior.PriceVersionID, &priorMenuID,&prior.ScheduledAt,&prior.CountryCode,&prior.RegionCode,&prior.CityCode,&prior.AreaCode)
	if err == nil {
		if priorMenuID != menuID || prior.ScheduledAt!=nil || prior.CountryCode!=nil { return Order{}, false, ErrOrderIdempotencyConflict }
		if err = tx.Commit(ctx); err != nil { return Order{}, false, err }
		return prior, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) { return Order{}, false, err }

	var version PriceVersion
	err = tx.QueryRow(ctx, `
		SELECT v.id,v.menu_id,v.version_no,v.amount_minor,v.currency,v.created_at
		FROM service_price_menus m
		JOIN service_price_versions v ON v.id=m.active_version_id
		WHERE m.id=$1
		FOR SHARE OF m
	`, menuID).Scan(&version.ID, &version.MenuID, &version.VersionNo, &version.AmountMinor, &version.Currency, &version.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) { return Order{}, false, ErrPriceMenuNotFound }
	if err != nil { return Order{}, false, err }
	var categoryID string
	if err = tx.QueryRow(ctx, `SELECT category_id FROM service_price_menus WHERE id=$1`, menuID).Scan(&categoryID); err != nil {
		return Order{}, false, err
	}
	enabled, err := categoryAvailableForOrder(ctx, tx, categoryID)
	if err != nil { return Order{}, false, err }
	if !enabled { return Order{}, false, ErrPriceUnavailable }

	id, err := newID()
	if err != nil { return Order{}, false, err }
	var createdID string
	err = tx.QueryRow(ctx, `
		INSERT INTO orders(id,customer_id,idempotency_key,amount_minor,currency,status,price_version_id)
		VALUES($1,$2,$3,$4,$5,'requested',$6)
		ON CONFLICT(customer_id,idempotency_key) DO NOTHING
		RETURNING id
	`, id, customerID, idempotencyKey, version.AmountMinor, version.Currency, version.ID).Scan(&createdID)
	if errors.Is(err, pgx.ErrNoRows) {
		var existing Order
		var existingMenuID string
		err = tx.QueryRow(ctx, `
			SELECT o.id,o.customer_id,o.amount_minor,o.currency,o.status,o.price_version_id,v.menu_id,
				o.scheduled_at,o.country_code,o.region_code,o.city_code,o.area_code
			FROM orders o
			JOIN service_price_versions v ON v.id=o.price_version_id
			WHERE o.customer_id=$1 AND o.idempotency_key=$2
		`, customerID, idempotencyKey).Scan(&existing.ID, &existing.CustomerID, &existing.AmountMinor,
			&existing.Currency, &existing.Status, &existing.PriceVersionID, &existingMenuID,
			&existing.ScheduledAt,&existing.CountryCode,&existing.RegionCode,&existing.CityCode,&existing.AreaCode)
		if errors.Is(err, pgx.ErrNoRows) { return Order{}, false, ErrOrderIdempotencyConflict }
		if err != nil { return Order{}, false, err }
		if existingMenuID != menuID || existing.ScheduledAt!=nil || existing.CountryCode!=nil {
			return Order{}, false, ErrOrderIdempotencyConflict
		}
		if err = tx.Commit(ctx); err != nil { return Order{}, false, err }
		return existing, false, nil
	}
	if err != nil { return Order{}, false, err }

	journalID := id
	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_journals(id,idempotency_key,currency,description)
		VALUES($1,$2,$3,'walking-skeleton simulated order hold')
	`, journalID, "order:"+id, version.Currency)
	if err != nil { return Order{}, false, fmt.Errorf("create test ledger journal: %w", err) }
	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_entries(journal_id,account_code,side,amount_minor)
		VALUES($1,'customer_funds','debit',$2),($1,'provider_payable','credit',$2)
	`, journalID, version.AmountMinor)
	if err != nil { return Order{}, false, fmt.Errorf("post balanced test ledger: %w", err) }
	payload := fmt.Sprintf(`{"order_id":%q,"status":"requested","price_version_id":%q}`, id, version.ID)
	if _, err = tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_type,aggregate_id,event_type,payload) VALUES('order',$1,'order.created',$2::jsonb)`, id, payload); err != nil {
		return Order{}, false, err
	}
	if err = tx.Commit(ctx); err != nil { return Order{}, false, err }
	return Order{ID:id,CustomerID:customerID,AmountMinor:version.AmountMinor,Currency:version.Currency,Status:"requested",PriceVersionID:&version.ID}, true, nil
}
