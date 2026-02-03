package aws

import (
	"net"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/go-logr/logr"
)

// isTransientError determines if an error is transient and should be retried
func isTransientError(err error) bool {
	// Check for network errors
	if isNetworkError(err) {
		return true
	}

	// Check for AWS SDK errors
	if awsErr, ok := err.(awserr.Error); ok {
		return isTransientAWSError(awsErr)
	}

	return false
}

// isNetworkError checks if the error is a network-related error
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.Error (includes timeouts, temporary errors, etc.)
	if netErr, ok := err.(net.Error); ok {
		// Timeouts and temporary errors should be retried
		if netErr.Timeout() || netErr.Temporary() {
			return true
		}
	}

	// Check for connection reset or refused
	errStr := err.Error()
	transientPatterns := []string{
		"connection reset",
		"connection refused",
		"connection timeout",
		"i/o timeout",
		"dial tcp",
		"TLS handshake timeout",
		"broken pipe",
		"unexpected EOF",
		"net.OpError",
	}

	for _, pattern := range transientPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// isTransientAWSError checks if an AWS error code represents a transient error
func isTransientAWSError(awsErr awserr.Error) bool {
	if awsErr == nil {
		return false
	}

	code := awsErr.Code()

	// Known transient AWS error codes
	transientCodes := map[string]bool{
		// Service-specific throttling and rate limiting
		"Throttling":               true,
		"ThrottlingException":      true,
		"TooManyRequestsException": true,
		"RequestLimitExceeded":     true,
		"SlowDown":                 true,

		// Service unavailable and internal errors
		"ServiceUnavailable":          true,
		"ServiceUnavailableException": true,
		"InternalFailure":             true,
		"InternalError":               true,
		"InternalServerError":         true,

		// Network and connectivity issues
		"RequestTimeout":             true,
		"RequestTimedOut":            true,
		"ConnectingTimeoutException": true,

		// Transient state errors
		"InvalidAction.NotFound":               true,
		"InvalidInstanceID.NotFound":           true,
		"InvalidGroup.NotFound":                true,
		"InstanceLimitExceeded":                true,
		"InsufficientInstanceCapacity":         true,
		"InsufficientReservedInstanceCapacity": true,

		// Generic client errors that might be transient
		"RequestExpired":        true,
		"InvalidInstance.State": true,
	}

	if transientCodes[code] {
		return true
	}

	// Check for operation-specific transient conditions
	// Some error messages indicate temporary issues even if the error code isn't explicitly transient
	message := awsErr.Message()
	transientMessages := []string{
		"resource is being updated",
		"in the process of being launched",
		"in the process of being created",
		"temporary unavailable",
		"temporarily unavailable",
		"try again",
	}

	for _, pattern := range transientMessages {
		if strings.Contains(strings.ToLower(message), pattern) {
			return true
		}
	}

	return false
}

// retryOnTransientError retries a function with exponential backoff if the error is transient
// Uses the default retry configuration
func retryOnTransientError(fn func() error, logger logr.Logger) error {
	return retryOnTransientErrorWithConfig(fn, logger, DefaultRetryConfig())
}

// retryOnTransientErrorWithConfig retries a function with exponential backoff if the error is transient
// Uses the provided retry configuration
func retryOnTransientErrorWithConfig(fn func() error, logger logr.Logger, config RetryConfig) error {
	// If retries are disabled, just execute once
	if !config.Enabled {
		return fn()
	}

	var lastErr error
	delayMs := config.InitialDelayMs

	for attempt := 0; attempt < config.MaxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		// Check if the error is transient
		if !isTransientError(err) {
			// Non-transient error, return immediately
			return err
		}

		lastErr = err

		// Log the transient error and retry attempt
		if logger != nil {
			logger.Info("Transient AWS error, retrying",
				"attempt", attempt+1,
				"maxRetries", config.MaxRetries,
				"delayMs", delayMs,
				"retryEnabled", config.Enabled,
				"error", err.Error())
		}

		// Don't sleep on the last attempt
		if attempt < config.MaxRetries-1 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)

			// Exponential backoff: double the delay (with random jitter would be better but simpler for now)
			delayMs *= 2
			if delayMs > config.MaxDelayMs {
				delayMs = config.MaxDelayMs
			}
		}
	}

	return lastErr
}
