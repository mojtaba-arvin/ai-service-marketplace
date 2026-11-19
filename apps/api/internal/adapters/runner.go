// Package adapters provides provider-neutral execution controls for external integrations.
package adapters

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidPolicy = errors.New("invalid adapter execution policy")
	ErrMissingIdempotencyKey = errors.New("idempotency key is required")
	ErrCircuitOpen = errors.New("provider circuit breaker is open")
)

// Policy bounds each provider attempt and the total retry budget. Retries are
// disabled unless Retryable explicitly classifies an error as safe to retry.
type Policy struct {
	Timeout time.Duration
	MaxAttempts int
	InitialBackoff time.Duration
	MaxBackoff time.Duration
	Retryable func(error) bool
}

func (p Policy) validate() error {
	if p.Timeout <= 0 || p.MaxAttempts < 1 || p.MaxAttempts > 10 ||
		p.InitialBackoff < 0 || p.MaxBackoff < p.InitialBackoff || p.Retryable == nil {
		return ErrInvalidPolicy
	}
	return nil
}

// Operation receives the same idempotency key on every attempt. Providers
// must persist/deduplicate that key before performing a non-idempotent effect.
type Operation[T any] func(context.Context, string) (T, error)

// Runner applies timeout, bounded retry, stable idempotency-key, and optional
// circuit-breaker controls to one provider operation.
type Runner struct {
	Policy Policy
	Breaker *CircuitBreaker
}

func (r Runner) Do[T any](ctx context.Context, key string, operation Operation[T]) (T, error) {
	var zero T
	if err := r.Policy.validate(); err != nil { return zero, err }
	if strings.TrimSpace(key) == "" { return zero, ErrMissingIdempotencyKey }
	if operation == nil { return zero, errors.New("adapter operation is required") }
	if err := ctx.Err(); err != nil { return zero, err }
	if r.Breaker != nil && !r.Breaker.allow(time.Now()) { return zero, ErrCircuitOpen }

	var lastErr error
	backoff := r.Policy.InitialBackoff
	for attempt := 1; attempt <= r.Policy.MaxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, r.Policy.Timeout)
		value, err := operation(attemptCtx, key)
		cancel()
		if err == nil {
			if r.Breaker != nil { r.Breaker.success() }
			return value, nil
		}
		lastErr = err
		if ctx.Err() != nil { lastErr = ctx.Err(); break }
		if attempt == r.Policy.MaxAttempts || !r.Policy.Retryable(err) { break }
		if backoff > 0 {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				if !timer.Stop() { <-timer.C }
				lastErr = ctx.Err()
				attempt = r.Policy.MaxAttempts
			case <-timer.C:
			}
		}
		if backoff < r.Policy.MaxBackoff {
			backoff *= 2
			if backoff > r.Policy.MaxBackoff { backoff = r.Policy.MaxBackoff }
		}
	}
	if r.Breaker != nil { r.Breaker.failure(time.Now()) }
	return zero, lastErr
}

type breakerState uint8
const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// CircuitBreaker permits one probe after the cooldown. It is safe for
// concurrent callers; calls already admitted before opening may finish.
type CircuitBreaker struct {
	mu sync.Mutex
	threshold int
	cooldown time.Duration
	failures int
	state breakerState
	openedAt time.Time
	probeInFlight bool
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) (*CircuitBreaker, error) {
	if threshold < 1 || cooldown <= 0 {
		return nil, fmt.Errorf("%w: breaker threshold and cooldown must be positive", ErrInvalidPolicy)
	}
	return &CircuitBreaker{threshold: threshold, cooldown: cooldown}, nil
}

func (b *CircuitBreaker) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerClosed:
		return true
	case breakerOpen:
		if now.Sub(b.openedAt) < b.cooldown { return false }
		b.state = breakerHalfOpen
		b.probeInFlight = true
		return true
	case breakerHalfOpen:
		return false
	default:
		return false
	}
}

func (b *CircuitBreaker) success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = breakerClosed
	b.failures = 0
	b.probeInFlight = false
}

func (b *CircuitBreaker) failure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == breakerHalfOpen {
		b.state = breakerOpen
		b.openedAt = now
		b.probeInFlight = false
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.state = breakerOpen
		b.openedAt = now
	}
}
