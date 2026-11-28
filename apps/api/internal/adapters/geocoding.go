package adapters

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
)

var (
	ErrInvalidGeocodeRequest = errors.New("invalid geocoding request")
	ErrGeocoderUnavailable = errors.New("geocoder unavailable")
)

type GeocodeRequest struct {
	MarketID string
	Locale string
	Address string
	IdempotencyKey string
}

type GeocodeResult struct {
	FormattedAddress string
	Latitude float64
	Longitude float64
	Provider string
	ProviderVersion string
}

type Geocoder interface {
	AdapterID() string
	Geocode(context.Context, GeocodeRequest) (GeocodeResult, error)
}

func ValidateGeocodeRequest(r GeocodeRequest) error {
	if strings.TrimSpace(r.MarketID) == "" || strings.TrimSpace(r.Locale) == "" ||
		strings.TrimSpace(r.Address) == "" || len(r.Address) > 1000 ||
		strings.TrimSpace(r.IdempotencyKey) == "" || len(r.IdempotencyKey) > 200 {
		return ErrInvalidGeocodeRequest
	}
	return nil
}

// Geocode runs a read-only provider lookup with a stable idempotency key.
func Geocode(ctx context.Context, runner Runner, provider Geocoder, request GeocodeRequest) (GeocodeResult, error) {
	if err := ValidateGeocodeRequest(request); err != nil { return GeocodeResult{}, err }
	if provider == nil { return GeocodeResult{}, ErrGeocoderUnavailable }
	return Do(runner, ctx, request.IdempotencyKey, func(ctx context.Context, key string) (GeocodeResult, error) {
		request.IdempotencyKey = key
		result, err := provider.Geocode(ctx, request)
		if err != nil {
			// Provider errors may contain the submitted address or other private
			// request data. Return only a stable, provider-neutral failure.
			return GeocodeResult{}, ErrGeocoderUnavailable
		}
		if strings.TrimSpace(result.FormattedAddress) == "" || result.Latitude < -90 || result.Latitude > 90 ||
			result.Longitude < -180 || result.Longitude > 180 || math.IsNaN(result.Latitude) || math.IsNaN(result.Longitude) ||
			math.IsInf(result.Latitude, 0) || math.IsInf(result.Longitude, 0) || result.Provider == "" || result.ProviderVersion == "" {
			return GeocodeResult{}, fmt.Errorf("%w: provider returned an invalid result", ErrGeocoderUnavailable)
		}
		return result, nil
	})
}

// FakeGeocoder is deterministic and intended only for unit and contract tests.
type FakeGeocoder struct {
	ID string
	Results map[string]GeocodeResult
	Calls int
	LastKey string
}

func (f *FakeGeocoder) AdapterID() string { return f.ID }

func (f *FakeGeocoder) Geocode(ctx context.Context, request GeocodeRequest) (GeocodeResult, error) {
	if err := ctx.Err(); err != nil { return GeocodeResult{}, err }
	f.Calls++
	f.LastKey = request.IdempotencyKey
	result, ok := f.Results[request.Address]
	if !ok { return GeocodeResult{}, ErrGeocoderUnavailable }
	return result, nil
}
