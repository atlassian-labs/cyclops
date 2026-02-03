package aws

import (
	"time"
)

// RetryConfig holds configuration for transient error retry behavior
type RetryConfig struct {
	// Enabled controls whether transient error retries are active
	Enabled bool

	// MaxRetries is the maximum number of retry attempts for transient errors
	MaxRetries int

	// InitialDelayMs is the initial delay in milliseconds before the first retry
	InitialDelayMs int

	// MaxDelayMs is the maximum delay in milliseconds between retry attempts
	MaxDelayMs int
}

// DefaultRetryConfig returns the default retry configuration
// This is the recommended configuration for production use
// Uses 5 second initial delay to be safe and avoid overwhelming AWS APIs
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		Enabled:        true,
		MaxRetries:     5,
		InitialDelayMs: 5000, // 5 seconds - safe default
		MaxDelayMs:     60000, // 60 seconds max
	}
}

// DisabledRetryConfig returns a configuration with retries disabled
// Use this for testing or to disable the retry feature
func DisabledRetryConfig() RetryConfig {
	return RetryConfig{
		Enabled:        false,
		MaxRetries:     1,
		InitialDelayMs: 0,
		MaxDelayMs:     0,
	}
}

// NewRetryConfig creates a new RetryConfig with custom values
func NewRetryConfig(enabled bool, maxRetries, initialDelayMs, maxDelayMs int) RetryConfig {
	return RetryConfig{
		Enabled:        enabled,
		MaxRetries:     maxRetries,
		InitialDelayMs: initialDelayMs,
		MaxDelayMs:     maxDelayMs,
	}
}

// Validate checks if the RetryConfig has valid values
func (rc RetryConfig) Validate() error {
	if !rc.Enabled {
		return nil // Disabled configs don't need validation
	}

	if rc.MaxRetries < 1 {
		return ErrInvalidRetryConfig("MaxRetries must be at least 1")
	}

	if rc.InitialDelayMs < 0 {
		return ErrInvalidRetryConfig("InitialDelayMs cannot be negative")
	}

	if rc.MaxDelayMs < rc.InitialDelayMs {
		return ErrInvalidRetryConfig("MaxDelayMs must be >= InitialDelayMs")
	}

	return nil
}

// GetInitialDelay returns the initial delay as a time.Duration
func (rc RetryConfig) GetInitialDelay() time.Duration {
	return time.Duration(rc.InitialDelayMs) * time.Millisecond
}

// GetMaxDelay returns the maximum delay as a time.Duration
func (rc RetryConfig) GetMaxDelay() time.Duration {
	return time.Duration(rc.MaxDelayMs) * time.Millisecond
}

// CustomError for retry configuration validation
type customError struct {
	message string
}

func (e customError) Error() string {
	return e.message
}

// ErrInvalidRetryConfig creates a new error for invalid retry configuration
func ErrInvalidRetryConfig(message string) error {
	return customError{message: message}
}
