package marketplace

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrCategoryNotFound = errors.New("service category not found")
	ErrCategoryDisabled = errors.New("service category or one of its ancestors is disabled")
	ErrInvalidCategory  = errors.New("invalid service category")
)

var categorySlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type ServiceCategory struct {
	ID          string     `json:"id"`
	ParentID    *string    `json:"parent_id"`
	Slug        string     `json:"slug"`
	DisplayName string     `json:"display_name"`
	Enabled     bool       `json:"enabled"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (s *Store) CreateCategory(ctx context.Context, parentID *string, slug, displayName string) (ServiceCategory, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	displayName = strings.TrimSpace(displayName)
	if !categorySlugPattern.MatchString(slug) || displayName == "" || len(displayName) > 160 {
		return ServiceCategory{}, ErrInvalidCategory
	}
	if parentID != nil {
		enabled, err := s.CanAcceptOrders(ctx, *parentID)
		if err != nil { return ServiceCategory{}, err }
		if !enabled { return ServiceCategory{}, ErrCategoryDisabled }
	}
	id, err := newID()
	if err != nil { return ServiceCategory{}, err }
	var category ServiceCategory
	err = s.DB.QueryRow(ctx, `
		INSERT INTO service_categories(id,parent_id,slug,display_name)
		VALUES($1,$2,$3,$4)
		RETURNING id,parent_id,slug,display_name,enabled,created_at,updated_at
	`, id, parentID, slug, displayName).Scan(&category.ID, &category.ParentID, &category.Slug,
		&category.DisplayName, &category.Enabled, &category.CreatedAt, &category.UpdatedAt)
	if err != nil { return ServiceCategory{}, fmt.Errorf("create service category: %w", err) }
	return category, nil
}

func (s *Store) GetCategory(ctx context.Context, id string) (ServiceCategory, error) {
	var category ServiceCategory
	err := s.DB.QueryRow(ctx, `
		SELECT id,parent_id,slug,display_name,enabled,created_at,updated_at
		FROM service_categories WHERE id=$1
	`, id).Scan(&category.ID, &category.ParentID, &category.Slug, &category.DisplayName,
		&category.Enabled, &category.CreatedAt, &category.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) { return ServiceCategory{}, ErrCategoryNotFound }
	if err != nil { return ServiceCategory{}, err }
	return category, nil
}

func (s *Store) SetCategoryEnabled(ctx context.Context, id string, enabled bool) error {
	if enabled {
		var parentID *string
		err := s.DB.QueryRow(ctx, `SELECT parent_id FROM service_categories WHERE id=$1`, id).Scan(&parentID)
		if errors.Is(err, pgx.ErrNoRows) { return ErrCategoryNotFound }
		if err != nil { return err }
		if parentID != nil {
			parentEnabled, checkErr := s.CanAcceptOrders(ctx, *parentID)
			if checkErr != nil { return checkErr }
			if !parentEnabled { return ErrCategoryDisabled }
		}
	}
	tag, err := s.DB.Exec(ctx, `UPDATE service_categories SET enabled=$2,updated_at=now() WHERE id=$1`, id, enabled)
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return ErrCategoryNotFound }
	return nil
}

func (s *Store) MoveCategory(ctx context.Context, id string, parentID *string) error {
	if id == "" { return ErrInvalidCategory }
	if parentID != nil {
		enabled, err := s.CanAcceptOrders(ctx, *parentID)
		if err != nil { return err }
		if !enabled { return ErrCategoryDisabled }
	}
	tag, err := s.DB.Exec(ctx, `UPDATE service_categories SET parent_id=$2,updated_at=now() WHERE id=$1`, id, parentID)
	if err != nil {
		if strings.Contains(err.Error(), "category hierarchy cannot contain a cycle") { return ErrInvalidCategory }
		return fmt.Errorf("move service category: %w", err)
	}
	if tag.RowsAffected() == 0 { return ErrCategoryNotFound }
	return nil
}

// CanAcceptOrders returns false when the category is missing, disabled, or has
// any disabled ancestor. The recursive query is the final server-side gate.
func (s *Store) CanAcceptOrders(ctx context.Context, id string) (bool, error) {
	var enabled bool
	err := s.DB.QueryRow(ctx, `
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id,enabled FROM service_categories WHERE id=$1
			UNION ALL
			SELECT c.id,c.parent_id,c.enabled
			FROM service_categories c JOIN ancestors a ON c.id=a.parent_id
		)
		SELECT COALESCE(bool_and(enabled),false) FROM ancestors
	`, id).Scan(&enabled)
	return enabled, err
}


// lockCategoryHierarchy serializes category mutations that must validate a
// parent/ancestor state before writing. This prevents a concurrent disable or
// move from racing an authorized create/move/enable operation.
func lockCategoryHierarchy(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('service-category-hierarchy',0))`)
	return err
}

func categoryHierarchyEnabledTx(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	var exists, enabled bool
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id,enabled FROM service_categories WHERE id=$1
			UNION ALL
			SELECT c.id,c.parent_id,c.enabled FROM service_categories c JOIN ancestors a ON c.id=a.parent_id
		)
		SELECT COUNT(*) > 0, COALESCE(bool_and(enabled),false) FROM ancestors
	`, id).Scan(&exists, &enabled)
	if err != nil { return false, err }
	if !exists { return false, ErrCategoryNotFound }
	return enabled, nil
}

// CreateCategoryForActor is the authorized application boundary for category
// creation. The low-level CreateCategory helper is retained for trusted
// internal setup and must not be exposed directly by HTTP handlers.
func (s *Store) CreateCategoryForActor(ctx context.Context, actor Actor, parentID *string, slug, displayName string) (ServiceCategory, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	displayName = strings.TrimSpace(displayName)
	if !categorySlugPattern.MatchString(slug) || displayName == "" || len(displayName) > 160 {
		return ServiceCategory{}, ErrInvalidCategory
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return ServiceCategory{}, err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: platformTenantID}, ActionCatalogManage); err != nil {
		return ServiceCategory{}, err
	}
	if err = lockCategoryHierarchy(ctx, tx); err != nil { return ServiceCategory{}, err }
	if parentID != nil {
		enabled, checkErr := categoryHierarchyEnabledTx(ctx, tx, *parentID)
		if checkErr != nil { return ServiceCategory{}, checkErr }
		if !enabled { return ServiceCategory{}, ErrCategoryDisabled }
	}
	id, err := newID()
	if err != nil { return ServiceCategory{}, err }
	var category ServiceCategory
	err = tx.QueryRow(ctx, `
		INSERT INTO service_categories(id,parent_id,slug,display_name)
		VALUES($1,$2,$3,$4)
		RETURNING id,parent_id,slug,display_name,enabled,created_at,updated_at
	`, id, parentID, slug, displayName).Scan(&category.ID, &category.ParentID, &category.Slug,
		&category.DisplayName, &category.Enabled, &category.CreatedAt, &category.UpdatedAt)
	if err != nil { return ServiceCategory{}, fmt.Errorf("create service category: %w", err) }
	if err = tx.Commit(ctx); err != nil { return ServiceCategory{}, err }
	return category, nil
}

// MoveCategoryForActor applies the same platform catalog-management grant and
// hierarchy serialization used by category creation and enablement.
func (s *Store) MoveCategoryForActor(ctx context.Context, actor Actor, id string, parentID *string) error {
	if strings.TrimSpace(id) == "" { return ErrInvalidCategory }
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: platformTenantID}, ActionCatalogManage); err != nil {
		return err
	}
	if err = lockCategoryHierarchy(ctx, tx); err != nil { return err }
	var categoryExists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_categories WHERE id=$1)`, id).Scan(&categoryExists); err != nil {
		return err
	}
	if !categoryExists { return ErrCategoryNotFound }
	if parentID != nil {
		enabled, checkErr := categoryHierarchyEnabledTx(ctx, tx, *parentID)
		if checkErr != nil { return checkErr }
		if !enabled { return ErrCategoryDisabled }
	}
	tag, err := tx.Exec(ctx, `UPDATE service_categories SET parent_id=$2,updated_at=now() WHERE id=$1`, id, parentID)
	if err != nil {
		if strings.Contains(err.Error(), "category hierarchy cannot contain a cycle") { return ErrInvalidCategory }
		return fmt.Errorf("move service category: %w", err)
	}
	if tag.RowsAffected() == 0 { return ErrCategoryNotFound }
	return tx.Commit(ctx)
}

// SetCategoryEnabledForActor is the authorized application boundary for
// category activation changes. Enabling beneath a disabled ancestor fails.
func (s *Store) SetCategoryEnabledForActor(ctx context.Context, actor Actor, id string, enabled bool) error {
	if strings.TrimSpace(id) == "" { return ErrInvalidCategory }
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil { return err }
	defer tx.Rollback(ctx)
	if err = authorizeOperatorTx(ctx, tx, actor, Resource{TenantID: platformTenantID}, ActionCatalogManage); err != nil {
		return err
	}
	if err = lockCategoryHierarchy(ctx, tx); err != nil { return err }
	if enabled {
		var parentID *string
		err = tx.QueryRow(ctx, `SELECT parent_id FROM service_categories WHERE id=$1`, id).Scan(&parentID)
		if errors.Is(err, pgx.ErrNoRows) { return ErrCategoryNotFound }
		if err != nil { return err }
		if parentID != nil {
			parentEnabled, checkErr := categoryHierarchyEnabledTx(ctx, tx, *parentID)
			if checkErr != nil { return checkErr }
			if !parentEnabled { return ErrCategoryDisabled }
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE service_categories SET enabled=$2,updated_at=now() WHERE id=$1`, id, enabled)
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return ErrCategoryNotFound }
	return tx.Commit(ctx)
}
