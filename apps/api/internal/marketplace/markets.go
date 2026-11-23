package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrMarketUnavailable = errors.New("market is unknown or disabled")
	ErrInvalidMarket = errors.New("invalid market configuration")
)

var (
	marketCodePattern = regexp.MustCompile(`^[A-Z]{2,8}$`)
	marketLocalePattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)
)

type MarketConfig struct {
	Code          string   `json:"code"`
	DefaultLocale string   `json:"default_locale"`
	Currency      string   `json:"currency"`
	Timezone      string   `json:"timezone"`
	PolicyVersion string   `json:"policy_version"`
	Enabled       bool     `json:"enabled"`
	Version       int      `json:"version"`
	Locales       []string `json:"locales"`
}

const platformTenantID = "platform"

// loadMarketConfigTx returns the full previous configuration, including disabled
// markets, while holding a row lock when the market already exists.
func loadMarketConfigTx(ctx context.Context, tx pgx.Tx, code string) (*MarketConfig, error) {
	var config MarketConfig
	err := tx.QueryRow(ctx, `
		SELECT code,default_locale,currency,timezone,policy_version,enabled,version
		FROM markets WHERE code=$1 FOR UPDATE
	`, code).Scan(&config.Code,&config.DefaultLocale,&config.Currency,&config.Timezone,&config.PolicyVersion,&config.Enabled,&config.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT locale FROM market_locales WHERE market_code=$1 ORDER BY locale`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var locale string
		if err := rows.Scan(&locale); err != nil {
			return nil, err
		}
		config.Locales = append(config.Locales, locale)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &config, nil
}

func normalizeMarketLocale(locale string) string {
	parts:=strings.Split(strings.TrimSpace(locale),"-")
	if len(parts)==1 { return strings.ToLower(parts[0]) }
	if len(parts)==2 { return strings.ToLower(parts[0])+"-"+strings.ToUpper(parts[1]) }
	return ""
}

func (s *Store) SaveMarket(ctx context.Context, actor Actor, config MarketConfig) (MarketConfig,error) {
	config.Code=strings.ToUpper(strings.TrimSpace(config.Code))
	config.DefaultLocale=normalizeMarketLocale(config.DefaultLocale)
	config.Currency=strings.ToUpper(strings.TrimSpace(config.Currency))
	config.Timezone=strings.TrimSpace(config.Timezone)
	config.PolicyVersion=strings.TrimSpace(config.PolicyVersion)
	// Market definitions are global, not tenant-owned. Only a platform-tenant
	// operator may mutate them.
	if err:=Authorize(actor,Resource{TenantID:platformTenantID},ActionMarketConfigure); err!=nil { return MarketConfig{},err }
	if !marketCodePattern.MatchString(config.Code) || !marketLocalePattern.MatchString(config.DefaultLocale) ||
		ValidateAmount(1,config.Currency)!=nil || config.PolicyVersion=="" || len(config.PolicyVersion)>80 {
		return MarketConfig{},ErrInvalidMarket
	}
	if _,err:=time.LoadLocation(config.Timezone); err!=nil { return MarketConfig{},ErrInvalidMarket }
	locales:=make(map[string]struct{},len(config.Locales))
	for _,locale:=range config.Locales {
		normalized:=normalizeMarketLocale(locale)
		if !marketLocalePattern.MatchString(normalized) { return MarketConfig{},ErrInvalidMarket }
		locales[normalized]=struct{}{}
	}
	if _,ok:=locales[config.DefaultLocale]; !ok { return MarketConfig{},ErrInvalidMarket }
	tx,err:=s.DB.BeginTx(ctx,pgx.TxOptions{})
	if err!=nil { return MarketConfig{},err }
	defer tx.Rollback(ctx)
	if _,err=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,"market-config:"+config.Code); err!=nil {
		return MarketConfig{},err
	}
	before,err:=loadMarketConfigTx(ctx,tx,config.Code)
	if err!=nil { return MarketConfig{},fmt.Errorf("read previous market config: %w",err) }
	err=tx.QueryRow(ctx,`
		INSERT INTO markets(code,default_locale,currency,timezone,policy_version,enabled)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(code) DO UPDATE SET
			default_locale=EXCLUDED.default_locale,currency=EXCLUDED.currency,
			timezone=EXCLUDED.timezone,policy_version=EXCLUDED.policy_version,
			enabled=EXCLUDED.enabled,version=markets.version+1,updated_at=now()
		RETURNING version
	`,config.Code,config.DefaultLocale,config.Currency,config.Timezone,config.PolicyVersion,config.Enabled).Scan(&config.Version)
	if err!=nil { return MarketConfig{},fmt.Errorf("save market: %w",err) }
	localeList:=make([]string,0,len(locales))
	for locale:=range locales { localeList=append(localeList,locale) }
	if _,err=tx.Exec(ctx,`DELETE FROM market_locales WHERE market_code=$1 AND NOT (locale = ANY($2::text[]))`,config.Code,localeList); err!=nil { return MarketConfig{},err }
	if _,err=tx.Exec(ctx,`UPDATE market_locales SET is_default=false WHERE market_code=$1`,config.Code); err!=nil { return MarketConfig{},err }
	for locale:=range locales {
		if _,err=tx.Exec(ctx,`
			INSERT INTO market_locales(market_code,locale,is_default) VALUES($1,$2,$3)
			ON CONFLICT(market_code,locale) DO UPDATE SET is_default=EXCLUDED.is_default
		`,config.Code,locale,locale==config.DefaultLocale); err!=nil {
			return MarketConfig{},err
		}
	}
	config.Locales=make([]string,0,len(locales))
	for locale:=range locales { config.Locales=append(config.Locales,locale) }
	sort.Strings(config.Locales)

	var beforeJSON []byte
	var previousVersion any
	action := "created"
	if before != nil {
		action = "updated"
		previousVersion = before.Version
		beforeJSON,err=json.Marshal(before)
		if err!=nil { return MarketConfig{},fmt.Errorf("encode previous market config: %w",err) }
	}
	afterJSON,err:=json.Marshal(config)
	if err!=nil { return MarketConfig{},fmt.Errorf("encode new market config: %w",err) }
	if _,err=tx.Exec(ctx,`
		INSERT INTO market_config_audit(
			market_code,actor_tenant_id,actor_subject_id,actor_role,action,
			previous_version,new_version,before_config,after_config
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`,config.Code,actor.TenantID,actor.SubjectID,string(actor.Role),action,previousVersion,config.Version,beforeJSON,afterJSON); err!=nil {
		return MarketConfig{},fmt.Errorf("append market config audit: %w",err)
	}
	if err=tx.Commit(ctx); err!=nil { return MarketConfig{},err }
	return config,nil
}

func (s *Store) GetMarket(ctx context.Context, code string) (MarketConfig,error) {
	code=strings.ToUpper(strings.TrimSpace(code))
	var config MarketConfig
	err:=s.DB.QueryRow(ctx,`
		SELECT code,default_locale,currency,timezone,policy_version,enabled,version
		FROM markets WHERE code=$1
	`,code).Scan(&config.Code,&config.DefaultLocale,&config.Currency,&config.Timezone,&config.PolicyVersion,&config.Enabled,&config.Version)
	if errors.Is(err,pgx.ErrNoRows) { return MarketConfig{},ErrMarketUnavailable }
	if err!=nil { return MarketConfig{},err }
	if !config.Enabled { return MarketConfig{},ErrMarketUnavailable }
	rows,err:=s.DB.Query(ctx,`SELECT locale FROM market_locales WHERE market_code=$1 ORDER BY locale`,code)
	if err!=nil { return MarketConfig{},err }
	defer rows.Close()
	for rows.Next() {
		var locale string
		if err=rows.Scan(&locale); err!=nil { return MarketConfig{},err }
		config.Locales=append(config.Locales,locale)
	}
	if err=rows.Err(); err!=nil { return MarketConfig{},err }
	return config,nil
}

func (s *Store) ResolveMarketLocale(ctx context.Context, marketCode, requestedLocale string) (MarketConfig,string,error) {
	config,err:=s.GetMarket(ctx,marketCode)
	if err!=nil { return MarketConfig{},"",err }
	requested:=normalizeMarketLocale(requestedLocale)
	for _,locale:=range config.Locales {
		if locale==requested { return config,locale,nil }
	}
	return config,config.DefaultLocale,nil
}
