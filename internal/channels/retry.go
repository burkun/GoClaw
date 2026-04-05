package channels

import (
	"context"
	"math/rand"
	"time"
)

// RetryConfig configures retry behavior for channel operations.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts (including initial).
	MaxAttempts int
	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration
	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration
	// Multiplier is the factor by which delay increases after each retry.
	Multiplier float64
}

// DefaultRetryConfig returns the default retry configuration aligned with DeerFlow.
// - 3 attempts total
// - Exponential backoff: 1s, 2s
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     4 * time.Second,
		Multiplier:   2.0,
	}
}

// Retry executes the given operation with exponential backoff retry.
// It returns the last error if all attempts fail.
func Retry(ctx context.Context, cfg RetryConfig, op func() error) error {
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context before attempting
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		default:
		}

		// Attempt the operation
		err := op()
		if err == nil {
			return nil
		}
		lastErr = err

		// Don't sleep after the last attempt
		if attempt >= cfg.MaxAttempts {
			break
		}

		// Add jitter to avoid thundering herd (±10%)
		jitter := time.Duration(float64(delay) * (0.9 + 0.2*rand.Float64()))
		sleepTime := min(jitter, cfg.MaxDelay)

		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(sleepTime):
		}

		// Increase delay for next attempt
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}

	return lastErr
}

// RetryWithResult executes the given operation with exponential backoff retry.
// It returns the result if successful, or the last error if all attempts fail.
func RetryWithResult[T any](ctx context.Context, cfg RetryConfig, op func() (T, error)) (T, error) {
	var lastErr error
	var result T
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context before attempting
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return result, lastErr
			}
			return result, ctx.Err()
		default:
		}

		// Attempt the operation
		res, err := op()
		if err == nil {
			return res, nil
		}
		lastErr = err

		// Don't sleep after the last attempt
		if attempt >= cfg.MaxAttempts {
			break
		}

		// Add jitter to avoid thundering herd (±10%)
		jitter := time.Duration(float64(delay) * (0.9 + 0.2*rand.Float64()))
		sleepTime := min(jitter, cfg.MaxDelay)

		select {
		case <-ctx.Done():
			return result, lastErr
		case <-time.After(sleepTime):
		}

		// Increase delay for next attempt
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}

	return result, lastErr
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
