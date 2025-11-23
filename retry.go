package switchfs

import (
	"math"
	"time"
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxAttempts     int           // Maximum number of retry attempts
	InitialDelay    time.Duration // Initial delay before first retry
	MaxDelay        time.Duration // Maximum delay between retries
	Multiplier      float64       // Backoff multiplier
	EnableFailover  bool          // Whether to try failover backend
	HealthMonitor   *HealthMonitor // Health monitor for circuit breaker
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:    3,
		InitialDelay:   100 * time.Millisecond,
		MaxDelay:       5 * time.Second,
		Multiplier:     2.0,
		EnableFailover: true,
	}
}

// RetryOperation represents an operation that can be retried
type RetryOperation func() error

// RetryWithBackoff retries an operation with exponential backoff
func RetryWithBackoff(config *RetryConfig, op RetryOperation) error {
	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// Try the operation
		err := op()
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't sleep after the last attempt
		if attempt < config.MaxAttempts-1 {
			time.Sleep(delay)

			// Calculate next delay with exponential backoff
			delay = time.Duration(float64(delay) * config.Multiplier)
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
		}
	}

	return lastErr
}

// CalculateBackoff calculates the backoff duration for a given attempt
func CalculateBackoff(attempt int, initialDelay, maxDelay time.Duration, multiplier float64) time.Duration {
	delay := time.Duration(float64(initialDelay) * math.Pow(multiplier, float64(attempt)))
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

// jitterDuration adds random jitter to a duration to avoid thundering herd
func jitterDuration(duration time.Duration, jitterFactor float64) time.Duration {
	if jitterFactor <= 0 {
		return duration
	}

	jitter := time.Duration(float64(duration) * jitterFactor * (2.0*float64(time.Now().UnixNano()%1000)/1000.0 - 1.0))
	return duration + jitter
}
