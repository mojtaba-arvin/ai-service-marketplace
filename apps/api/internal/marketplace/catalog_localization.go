package marketplace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrCategoryUnavailable = errors.New("category is unavailable in this market or locale")

type LocalizedCategory struct {
	ID          string     `json:"id"`
	ParentID    *string    `json:"parent_id"`
	Slug        string     `json:"slug"`
	DisplayName string     `json:"display_name"`
	Description string     `json:"description"`
	MarketCode  string     `json:"market_code"`
	Locale      string     `json:"locale"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (s *Store) SetCategoryVisibility(ctx context.Context, actor Actor, marketCode, categoryID string, visible bool) error {
	if err:=Authorize(actor,Resource{TenantID:actor.TenantID},ActionCatalogManage); err!=nil { return err }
	market,err:=s.GetMarket(ctx,marketCode)
	if err!=nil { return err }
	if _,err=s.GetCategory(ctx,categoryID); err!=nil { return err }
	_,err=s.DB.Exec(ctx,`
		INSERT INTO market_category_visibility(market_code,category_id,visible)
		VALUES($1,$2,$3)
		ON CONFLICT(market_code,category_id) DO UPDATE
		SET visible=EXCLUDED.visible,updated_at=now()
	`,market.Code,categoryID,visible)
	return err
}

func (s *Store) SetCategoryTranslation(ctx context.Context, actor Actor, marketCode, categoryID, locale, displayName, description string) error {
	if err:=Authorize(actor,Resource{TenantID:actor.TenantID},ActionCatalogManage); err!=nil { return err }
	market,err:=s.GetMarket(ctx,marketCode)
	if err!=nil { return err }
	locale=normalizeMarketLocale(locale)
	supported:=false
	for _,candidate:=range market.Locales { if candidate==locale { supported=true; break } }
	displayName=strings.TrimSpace(displayName)
	if !supported || displayName=="" || len(displayName)>160 || len(description)>2000 {
		return ErrInvalidCategory
	}
	if _,err=s.GetCategory(ctx,categoryID); err!=nil { return err }
	_,err=s.DB.Exec(ctx,`
		INSERT INTO category_translations(market_code,category_id,locale,display_name,description)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(market_code,category_id,locale) DO UPDATE
		SET display_name=EXCLUDED.display_name,description=EXCLUDED.description,updated_at=now()
	`,market.Code,categoryID,locale,displayName,description)
	return err
}

func (s *Store) ResolveLocalizedCategory(ctx context.Context, marketCode, locale, categoryID string) (LocalizedCategory,error) {
	market,resolvedLocale,err:=s.ResolveMarketLocale(ctx,marketCode,locale)
	if err!=nil { return LocalizedCategory{},err }
	var category LocalizedCategory
	err=s.DB.QueryRow(ctx,`
		WITH RECURSIVE ancestors AS (
			SELECT id,parent_id,enabled FROM service_categories WHERE id=$1
			UNION ALL
			SELECT c.id,c.parent_id,c.enabled FROM service_categories c JOIN ancestors a ON c.id=a.parent_id
		)
		SELECT c.id,c.parent_id,c.slug,t.display_name,t.description,$2,$3,t.updated_at
		FROM service_categories c
		JOIN market_category_visibility v ON v.category_id=c.id AND v.market_code=$2 AND v.visible=true
		JOIN category_translations t ON t.category_id=c.id AND t.market_code=$2 AND t.locale=$3
		WHERE c.id=$1 AND c.enabled AND (SELECT COALESCE(bool_and(enabled),false) FROM ancestors)
	`,categoryID,market.Code,resolvedLocale).Scan(&category.ID,&category.ParentID,&category.Slug,&category.DisplayName,
		&category.Description,&category.MarketCode,&category.Locale,&category.UpdatedAt)
	if errors.Is(err,pgx.ErrNoRows) { return LocalizedCategory{},ErrCategoryUnavailable }
	if err!=nil { return LocalizedCategory{},err }
	return category,nil
}

func (s *Store) ListLocalizedCategories(ctx context.Context, marketCode, locale string, parentID *string) ([]LocalizedCategory,error) {
	market,resolvedLocale,err:=s.ResolveMarketLocale(ctx,marketCode,locale)
	if err!=nil { return nil,err }
	rows,err:=s.DB.Query(ctx,`
		SELECT c.id,c.parent_id,c.slug,t.display_name,t.description,$1,$2,t.updated_at
		FROM service_categories c
		JOIN market_category_visibility v ON v.category_id=c.id AND v.market_code=$1 AND v.visible=true
		JOIN category_translations t ON t.category_id=c.id AND t.market_code=$1 AND t.locale=$2
		WHERE c.enabled AND ($3::text IS NULL OR c.parent_id=$3)
		  AND NOT EXISTS (
			WITH RECURSIVE ancestors AS (
				SELECT id,parent_id,enabled FROM service_categories WHERE id=c.id
				UNION ALL
				SELECT p.id,p.parent_id,p.enabled FROM service_categories p JOIN ancestors a ON p.id=a.parent_id
			)
			SELECT 1 FROM ancestors WHERE enabled=false
		  )
		ORDER BY c.parent_id NULLS FIRST,c.slug,c.id
	`,market.Code,resolvedLocale,parentID)
	if err!=nil { return nil,err }
	defer rows.Close()
	categories:=make([]LocalizedCategory,0)
	for rows.Next() {
		var category LocalizedCategory
		if err=rows.Scan(&category.ID,&category.ParentID,&category.Slug,&category.DisplayName,&category.Description,
			&category.MarketCode,&category.Locale,&category.UpdatedAt); err!=nil { return nil,err }
		categories=append(categories,category)
	}
	if err=rows.Err(); err!=nil { return nil,fmt.Errorf("list localized categories: %w",err) }
	return categories,nil
}
