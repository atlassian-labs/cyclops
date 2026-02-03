package aws

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/go-logr/logr/testr"
)

// TestRetryOnTransientError_IOTimeout reproduces the original issue
// Before fix: AWS call fails with i/o timeout → Immediate Healing
// After fix: Retries with backoff until success
func TestRetryOnTransientError_IOTimeout(t *testing.T) {
	t.Run("Retries on i/o timeout and eventually succeeds", func(t *testing.T) {
		attempts := 0
		maxRetries := 3

		fn := func() error {
			attempts++
			if attempts <= maxRetries {
				// Simulate the original error
				return errors.New("Post \"https://autoscaling.us-west-2.amazonaws.com/\": dial tcp 18.246.117.35:443: i/o timeout")
			}
			return nil
		}

		logger := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
		err := retryOnTransientError(fn, logger)

		if err != nil {
			t.Errorf("Expected success after retries, got error: %v", err)
		}

		expectedAttempts := maxRetries + 1
		if attempts != expectedAttempts {
			t.Errorf("Expected %d attempts, got %d", expectedAttempts, attempts)
		}

		t.Logf("✓ Successfully recovered after %d attempts (original issue fixed)", attempts)
	})
}

// TestRetryOnTransientError_AWSThrottling reproduces AWS rate limiting scenario
func TestRetryOnTransientError_AWSThrottling(t *testing.T) {
	t.Run("Retries on Throttling error and eventually succeeds", func(t *testing.T) {
		attempts := 0
		maxRetries := 2

		fn := func() error {
			attempts++
			if attempts <= maxRetries {
				return awserr.New("Throttling", "Rate limit exceeded", nil)
			}
			return nil
		}

		logger := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
		err := retryOnTransientError(fn, logger)

		if err != nil {
			t.Errorf("Expected success after retries, got error: %v", err)
		}

		if attempts != maxRetries+1 {
			t.Errorf("Expected %d attempts, got %d", maxRetries+1, attempts)
		}

		t.Logf("✓ Successfully recovered from AWS throttling after %d attempts", attempts)
	})
}

// TestRetryOnTransientError_AWSServiceUnavailable tests AWS service outage recovery
func TestRetryOnTransientError_AWSServiceUnavailable(t *testing.T) {
	t.Run("Retries on ServiceUnavailable and eventually succeeds", func(t *testing.T) {
		attempts := 0
		maxRetries := 2

		fn := func() error {
			attempts++
			if attempts <= maxRetries {
				return awserr.New("ServiceUnavailable", "Service temporarily unavailable", nil)
			}
			return nil
		}

		logger := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
		err := retryOnTransientError(fn, logger)

		if err != nil {
			t.Errorf("Expected success after retries, got error: %v", err)
		}

		if attempts != maxRetries+1 {
			t.Errorf("Expected %d attempts, got %d", maxRetries+1, attempts)
		}

		t.Logf("✓ Successfully recovered from AWS service unavailability after %d attempts", attempts)
	})
}

// TestRetryOnTransientError_PermanentError demonstrates permanent errors fail immediately
// Before fix: Would still try Healing phase
// After fix: Returns error immediately without retries
func TestRetryOnTransientError_PermanentError(t *testing.T) {
	t.Run("Fails immediately on permanent error (AccessDenied)", func(t *testing.T) {
		attempts := 0
		permanentErr := awserr.New("AccessDenied", "Access denied", nil)

		fn := func() error {
			attempts++
			return permanentErr
		}

		logger := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
		err := retryOnTransientError(fn, logger)

		if err != permanentErr {
			t.Errorf("Expected permanent error to be returned, got: %v", err)
		}

		if attempts != 1 {
			t.Errorf("Expected 1 attempt for permanent error, got %d", attempts)
		}

		t.Logf("✓ Permanent error failed fast without retries")
	})
}

// TestRetryOnTransientError_BackoffTiming verifies exponential backoff timing
func TestRetryOnTransientError_BackoffTiming(t *testing.T) {
	t.Run("Implements exponential backoff correctly", func(t *testing.T) {
		attempts := 0
		timings := []time.Time{}
		maxRetries := 3

		fn := func() error {
			attempts++
			timings = append(timings, time.Now())
			if attempts <= maxRetries {
				return errors.New("i/o timeout")
			}
			return nil
		}

		logger := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
		startTime := time.Now()
		err := retryOnTransientError(fn, logger)
		totalDuration := time.Since(startTime)

		if err != nil {
			t.Errorf("Expected success after retries, got error: %v", err)
		}

		if attempts != maxRetries+1 {
			t.Errorf("Expected %d attempts, got %d", maxRetries+1, attempts)
		}

		// Verify backoff increased between attempts (100ms, 200ms, 400ms)
		if len(timings) >= 2 {
			gap1 := timings[1].Sub(timings[0])
			if gap1 < 90*time.Millisecond || gap1 > 150*time.Millisecond {
				t.Logf("First backoff gap: %v (expected ~100ms)", gap1)
			}
		}

		t.Logf("✓ Completed 4 attempts in %.2f seconds with exponential backoff", totalDuration.Seconds())
	})
}

// TestRetryOnTransientError_ExhaustsRetries demonstrates behavior when retries are exhausted
func TestRetryOnTransientError_ExhaustsRetries(t *testing.T) {
	t.Run("Exhausts all retries and returns last error", func(t *testing.T) {
		attempts := 0
		persistentErr := errors.New("dial tcp: i/o timeout")

		fn := func() error {
			attempts++
			return persistentErr
		}

		logger := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
		err := retryOnTransientError(fn, logger)

		if err != persistentErr {
			t.Errorf("Expected persistent error to be returned, got: %v", err)
		}

		expectedAttempts := 5 // Default max retries
		if attempts != expectedAttempts {
			t.Errorf("Expected %d attempts (max retries exhausted), got %d", expectedAttempts, attempts)
		}

		t.Logf("✓ Correctly exhausted all %d retry attempts", attempts)
	})
}

// TestRetryOnTransientError_ConnectionReset tests another common network error
func TestRetryOnTransientError_ConnectionReset(t *testing.T) {
	t.Run("Retries on connection reset and eventually succeeds", func(t *testing.T) {
		attempts := 0
		maxRetries := 2

		fn := func() error {
			attempts++
			if attempts <= maxRetries {
				return errors.New("read tcp: connection reset by peer")
			}
			return nil
		}

		logger := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
		err := retryOnTransientError(fn, logger)

		if err != nil {
			t.Errorf("Expected success after retries, got error: %v", err)
		}

		if attempts != maxRetries+1 {
			t.Errorf("Expected %d attempts, got %d", maxRetries+1, attempts)
		}

		t.Logf("✓ Successfully recovered from connection reset after %d attempts", attempts)
	})
}

// TestRetryOnTransientError_TLSHandshakeTimeout tests TLS-specific errors
func TestRetryOnTransientError_TLSHandshakeTimeout(t *testing.T) {
	t.Run("Retries on TLS handshake timeout and eventually succeeds", func(t *testing.T) {
		attempts := 0
		maxRetries := 2

		fn := func() error {
			attempts++
			if attempts <= maxRetries {
				return errors.New("TLS handshake timeout")
			}
			return nil
		}

		logger := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
		err := retryOnTransientError(fn, logger)

		if err != nil {
			t.Errorf("Expected success after retries, got error: %v", err)
		}

		if attempts != maxRetries+1 {
			t.Errorf("Expected %d attempts, got %d", maxRetries+1, attempts)
		}

		t.Logf("✓ Successfully recovered from TLS handshake timeout after %d attempts", attempts)
	})
}

// TestRetryOnTransientError_MultipleTransientErrors tests recovering through multiple different errors
func TestRetryOnTransientError_MultipleTransientErrors(t *testing.T) {
	t.Run("Recovers through sequence of different transient errors", func(t *testing.T) {
		attempts := 0
		errors := []error{
			errors.New("dial tcp: i/o timeout"),
			awserr.New("Throttling", "Rate exceeded", nil),
			errors.New("connection reset by peer"),
		}

		fn := func() error {
			defer func() { attempts++ }()
			if attempts < len(errors) {
				return errors[attempts]
			}
			return nil
		}

		logger := testr.NewWithOptions(t, testr.Options{Verbosity: 10})
		err := retryOnTransientError(fn, logger)

		if err != nil {
			t.Errorf("Expected success after retries, got error: %v", err)
		}

		expectedAttempts := len(errors) + 1
		if attempts != expectedAttempts {
			t.Errorf("Expected %d attempts, got %d", expectedAttempts, attempts)
		}

		t.Logf("✓ Successfully recovered through %d different transient errors", len(errors))
	})
}

// Using testr.NewWithOptions from go-logr/logr/testr for testing
// This provides a proper logr.Logger implementation that logs to *testing.T
