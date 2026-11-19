// Package marketconfig provides a fail-closed, immutable market configuration registry.
package marketconfig

import (
 "errors"
 "fmt"
 "regexp"
 "strings"
 "time"
)

var (
 ErrUnknownMarket = errors.New("unknown market configuration")
 ErrInvalidConfig = errors.New("invalid market configuration")
 marketPattern = regexp.MustCompile(`^[A-Z]{2}$`)
 localePattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)
 currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
 policyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

type Config struct {
 MarketID string `json:"market_id"`
 Locale string `json:"locale"`
 Currency string `json:"currency"`
 TimeZone string `json:"time_zone"`
 PolicyVersion string `json:"policy_version"`
}
type key struct { market, locale string }
type Registry struct { entries map[key]Config }

func New(configs []Config) (*Registry,error) {
 if len(configs)==0 { return nil,fmt.Errorf("%w: at least one entry is required",ErrInvalidConfig) }
 entries:=make(map[key]Config,len(configs))
 for _,cfg:=range configs {
  if err:=validate(cfg); err!=nil { return nil,err }
  k:=key{market:cfg.MarketID,locale:cfg.Locale}
  if _,ok:=entries[k];ok { return nil,fmt.Errorf("%w: duplicate market/locale %s/%s",ErrInvalidConfig,cfg.MarketID,cfg.Locale) }
  entries[k]=cfg
 }
 return &Registry{entries:entries},nil
}
func validate(cfg Config) error {
 if !marketPattern.MatchString(cfg.MarketID)||!localePattern.MatchString(cfg.Locale)||!currencyPattern.MatchString(cfg.Currency)||!policyPattern.MatchString(cfg.PolicyVersion) {
  return fmt.Errorf("%w: market, locale, currency, or policy version has invalid format",ErrInvalidConfig)
 }
 if strings.TrimSpace(cfg.TimeZone)!=cfg.TimeZone { return fmt.Errorf("%w: invalid time zone",ErrInvalidConfig) }
 if _,err:=time.LoadLocation(cfg.TimeZone);err!=nil { return fmt.Errorf("%w: invalid time zone: %v",ErrInvalidConfig,err) }
 return nil
}
// Resolve requires an exact market and locale match; no fallback is performed.
func (r *Registry) Resolve(marketID,locale string)(Config,error) {
 if r==nil||marketID==""||locale==""||strings.TrimSpace(marketID)!=marketID||strings.TrimSpace(locale)!=locale { return Config{},ErrUnknownMarket }
 cfg,ok:=r.entries[key{market:marketID,locale:locale}]
 if !ok { return Config{},fmt.Errorf("%w: %s/%s",ErrUnknownMarket,marketID,locale) }
 return cfg,nil
}
