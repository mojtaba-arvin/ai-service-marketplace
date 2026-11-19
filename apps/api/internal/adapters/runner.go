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
// must deduplicate that key before performing a non-idempotent effect.
type Operation[T any] func(context.Context, string) (T, error)

type Runner struct {
	Policy Policy
	Breaker *CircuitBreaker
}

// Do applies bounded attempts and per-attempt timeouts. Retryability is an
// explicit caller decision; every retry reuses the same idempotency key.
func Do[T any](r Runner, ctx context.Context, key string, operation Operation[T]) (T, error) {
	var zero T
	if err := r.Policy.validate(); err != nil { return zero, err }
	if strings.TrimSpace(key) == "" { return zero, ErrMissingIdempotencyKey }
	if operation == nil { return zero, errors.New("adapter operation is required") }
	if err := ctx.Err(); err != nil { return zero, err }
	var permit breakerPermit
	if r.Breaker != nil {
		var allowed bool
		permit, allowed = r.Breaker.allow(time.Now())
		if !allowed { return zero, ErrCircuitOpen }
	}

	var lastErr error
	backoff := r.Policy.InitialBackoff
	for attempt := 1; attempt <= r.Policy.MaxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, r.Policy.Timeout)
		value, err := operation(attemptCtx, key)
		cancel()
		if err == nil {
			if r.Breaker != nil { r.Breaker.success(permit) }
			return value, nil
		}
		lastErr = err
		if ctx.Err() != nil { lastErr = ctx.Err(); break }
		if attempt == r.Policy.MaxAttempts || !r.Policy.Retryable(err) { break }
		if backoff > 0 {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				if !timer.Stop() { select { case <-timer.C: default: } }
				lastErr = ctx.Err()
				attempt = r.Policy.MaxAttempts
			case <-timer.C:
			}
		}
		if backoff > 0 && backoff < r.Policy.MaxBackoff {
			backoff *= 2
			if backoff > r.Policy.MaxBackoff { backoff = r.Policy.MaxBackoff }
		}
	}
	if r.Breaker != nil {
		if ctx.Err() == nil && r.Policy.Retryable(lastErr) { r.Breaker.failure(permit, time.Now()) } else { r.Breaker.success(permit) }
	}
	return zero, lastErr
}

type breakerState uint8
const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// CircuitBreaker admits one half-open probe after cooldown. It is safe for
// concurrent callers; operations admitted before opening may finish.
type breakerPermit struct { generation uint64; halfOpen bool }

type CircuitBreaker struct {
	mu sync.Mutex
	threshold int
	cooldown time.Duration
	failures int
	state breakerState
	openedAt time.Time
	generation uint64
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) (*CircuitBreaker, error) {
	if threshold < 1 || cooldown <= 0 {
		return nil, fmt.Errorf("%w: breaker threshold and cooldown must be positive", ErrInvalidPolicy)
	}
	return &CircuitBreaker{threshold: threshold, cooldown: cooldown}, nil
}

func (b *CircuitBreaker) allow(now time.Time) (breakerPermit, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerClosed:
		return breakerPermit{generation:b.generation}, true
	case breakerOpen:
		if now.Sub(b.openedAt) < b.cooldown { return breakerPermit{}, false }
		b.state = breakerHalfOpen
		b.generation++
		return breakerPermit{generation:b.generation,halfOpen:true}, true
	case breakerHalfOpen:
		return breakerPermit{}, false
	default:
		return breakerPermit{}, false
	}
}

func (b *CircuitBreaker) success(permit breakerPermit) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if permit.generation != b.generation { return }
	if permit.halfOpen {
		if b.state != breakerHalfOpen { return }
		b.state = breakerClosed
		b.generation++
	} else if b.state != breakerClosed { return }
	b.failures = 0
}

func (b *CircuitBreaker) failure(permit breakerPermit, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if permit.generation != b.generation { return }
	if permit.halfOpen {
		if b.state != breakerHalfOpen { return }
		b.state = breakerOpen
		b.openedAt = now
		b.generation++
		return
	}
	if b.state != breakerClosed { return }
	b.failures++
	if b.failures >= b.threshold {
		b.state = breakerOpen
		b.openedAt = now
		b.generation++
	}
}
