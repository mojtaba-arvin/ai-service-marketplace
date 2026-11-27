// Package marketconfig provides a fail-closed, immutable market configuration registry.
package marketconfig

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type Direction string

const (
	DirectionLTR Direction = "ltr"
	DirectionRTL Direction = "rtl"
)

var rtlLanguages = map[string]struct{}{
	"ar": {}, "fa": {}, "he": {}, "ur": {}, "ps": {}, "sd": {}, "ug": {}, "yi": {},
}

// Direction resolves the writing direction from the configured locale's
// language subtag. It never guesses from a market or falls back to a default.
func (c Config) Direction() (Direction, error) {
	if !localePattern.MatchString(c.Locale) {
		return "", fmt.Errorf("%w: invalid locale %q", ErrInvalidConfig, c.Locale)
	}
	language := strings.SplitN(c.Locale, "-", 2)[0]
	if _, ok := rtlLanguages[language]; ok {
		return DirectionRTL, nil
	}
	return DirectionLTR, nil
}

var capabilityNamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

var (
	ErrAdapterUnavailable = errors.New("adapter unavailable for market")
	ErrUnknownCapability  = errors.New("unknown adapter capability")
)

type AdapterConfig struct {
	MarketID      string
	Locale        string
	PolicyVersion string
	Adapter       string
	Capabilities  []string
}

type adapterKey struct {
	marketLocale key
	adapter      string
}

type Matrix struct {
	registry *Registry
	entries  map[adapterKey]map[string]struct{}
}

// NewMatrix builds an immutable, explicit market/locale-to-adapter capability
// matrix. Every row must reference the exact policy version in the registry.
// An adapter with an empty capability list is available but supports no optional feature.
func NewMatrix(registry *Registry, configs []AdapterConfig) (*Matrix, error) {
	if registry == nil || len(configs) == 0 {
		return nil, fmt.Errorf("%w: registry and at least one adapter row are required", ErrInvalidConfig)
	}
	entries := make(map[adapterKey]map[string]struct{}, len(configs))
	for _, cfg := range configs {
		market, err := registry.Resolve(cfg.MarketID, cfg.Locale)
		if err != nil {
			return nil, err
		}
		if cfg.PolicyVersion == "" || cfg.PolicyVersion != market.PolicyVersion {
			return nil, fmt.Errorf("%w: adapter policy version does not match market configuration", ErrInvalidConfig)
		}
		if !capabilityNamePattern.MatchString(cfg.Adapter) {
			return nil, fmt.Errorf("%w: invalid adapter identifier %q", ErrInvalidConfig, cfg.Adapter)
		}
		k := adapterKey{marketLocale: key{market: cfg.MarketID, locale: cfg.Locale}, adapter: cfg.Adapter}
		if _, exists := entries[k]; exists {
			return nil, fmt.Errorf("%w: duplicate adapter row %s/%s/%s", ErrInvalidConfig, cfg.MarketID, cfg.Locale, cfg.Adapter)
		}
		capabilities := make(map[string]struct{}, len(cfg.Capabilities))
		for _, capability := range cfg.Capabilities {
			if !capabilityNamePattern.MatchString(capability) {
				return nil, fmt.Errorf("%w: invalid capability identifier %q", ErrInvalidConfig, capability)
			}
			if _, exists := capabilities[capability]; exists {
				return nil, fmt.Errorf("%w: duplicate capability %q", ErrInvalidConfig, capability)
			}
			capabilities[capability] = struct{}{}
		}
		entries[k] = capabilities
	}
	return &Matrix{registry: registry, entries: entries}, nil
}

// Require verifies both market availability and a named adapter capability.
// It deliberately does not infer support from another locale or market.
func (m *Matrix) Require(marketID, locale, adapter, capability string) error {
	if m == nil || m.registry == nil {
		return ErrUnknownMarket
	}
	if _, err := m.registry.Resolve(marketID, locale); err != nil {
		return err
	}
	if !capabilityNamePattern.MatchString(adapter) || !capabilityNamePattern.MatchString(capability) {
		return fmt.Errorf("%w: invalid adapter or capability identifier", ErrUnknownCapability)
	}
	capabilities, ok := m.entries[adapterKey{
		marketLocale: key{market: marketID, locale: locale},
		adapter: adapter,
	}]
	if !ok {
		return fmt.Errorf("%w: %s in %s/%s", ErrAdapterUnavailable, adapter, marketID, locale)
	}
	if _, ok := capabilities[capability]; !ok {
		return fmt.Errorf("%w: %s does not support %s in %s/%s", ErrUnknownCapability, adapter, capability, marketID, locale)
	}
	return nil
}
