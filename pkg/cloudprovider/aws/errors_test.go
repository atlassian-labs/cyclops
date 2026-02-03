package aws

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
)

// mockNetError is a mock implementation of net.Error for testing
type mockNetError struct {
	timeout   bool
	temporary bool
	msg       string
}

func (e *mockNetError) Error() string   { return e.msg }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return e.temporary }

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "timeout error",
			err:      &mockNetError{timeout: true, msg: "timeout"},
			expected: true,
		},
		{
			name:     "temporary error",
			err:      &mockNetError{temporary: true, msg: "temporary"},
			expected: true,
		},
		{
			name:     "i/o timeout string",
			err:      errors.New("Post \"https://autoscaling.us-west-2.amazonaws.com/\": dial tcp 18.246.117.35:443: i/o timeout"),
			expected: true,
		},
		{
			name:     "connection refused",
			err:      errors.New("dial tcp: connection refused"),
			expected: true,
		},
		{
			name:     "AWS throttling error",
			err:      awserr.New("Throttling", "Rate exceeded", nil),
			expected: true,
		},
		{
			name:     "AWS service unavailable",
			err:      awserr.New("ServiceUnavailable", "Service temporarily unavailable", nil),
			expected: true,
		},
		{
			name:     "AWS internal error",
			err:      awserr.New("InternalFailure", "Internal service error", nil),
			expected: true,
		},
		{
			name:     "non-transient AWS error",
			err:      awserr.New("AccessDenied", "Access denied", nil),
			expected: false,
		},
		{
			name:     "non-transient error",
			err:      errors.New("invalid input parameter"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientError(tt.err)
			if result != tt.expected {
				t.Errorf("isTransientError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestRetryOnTransientError(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		attempts := 0
		fn := func() error {
			attempts++
			return nil
		}

		err := retryOnTransientError(fn, nil)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if attempts != 1 {
			t.Errorf("expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("succeeds after transient errors", func(t *testing.T) {
		attempts := 0
		fn := func() error {
			attempts++
			if attempts < 3 {
				return errors.New("i/o timeout")
			}
			return nil
		}

		err := retryOnTransientError(fn, nil)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if attempts != 3 {
			t.Errorf("expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("fails immediately on non-transient error", func(t *testing.T) {
		attempts := 0
		nonTransientErr := errors.New("invalid parameter")
		fn := func() error {
			attempts++
			return nonTransientErr
		}

		err := retryOnTransientError(fn, nil)
		if err != nonTransientErr {
			t.Errorf("expected non-transient error, got %v", err)
		}
		if attempts != 1 {
			t.Errorf("expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("exhausts retries on persistent transient errors", func(t *testing.T) {
		attempts := 0
		transientErr := errors.New("i/o timeout")
		fn := func() error {
			attempts++
			return transientErr
		}

		err := retryOnTransientError(fn, nil)
		if err != transientErr {
			t.Errorf("expected transient error after exhausting retries, got %v", err)
		}
		if attempts != 5 {
			t.Errorf("expected 5 attempts, got %d", attempts)
		}
	})
}

func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "timeout error",
			err:      &mockNetError{timeout: true, msg: "timeout"},
			expected: true,
		},
		{
			name:     "temporary error",
			err:      &mockNetError{temporary: true, msg: "temp"},
			expected: true,
		},
		{
			name:     "dial tcp error",
			err:      errors.New("dial tcp 1.2.3.4:443: i/o timeout"),
			expected: true,
		},
		{
			name:     "connection reset",
			err:      errors.New("read tcp: connection reset by peer"),
			expected: true,
		},
		{
			name:     "non-network error",
			err:      errors.New("something else"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNetworkError(tt.err)
			if result != tt.expected {
				t.Errorf("isNetworkError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestIsTransientAWSError(t *testing.T) {
	tests := []struct {
		name     string
		err      awserr.Error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "throttling error",
			err:      awserr.New("Throttling", "Rate limit exceeded", nil),
			expected: true,
		},
		{
			name:     "service unavailable",
			err:      awserr.New("ServiceUnavailable", "Service is unavailable", nil),
			expected: true,
		},
		{
			name:     "internal error",
			err:      awserr.New("InternalError", "Internal server error", nil),
			expected: true,
		},
		{
			name:     "request timeout",
			err:      awserr.New("RequestTimeout", "Request timed out", nil),
			expected: true,
		},
		{
			name:     "access denied - non-transient",
			err:      awserr.New("AccessDenied", "Access denied", nil),
			expected: false,
		},
		{
			name:     "validation error - non-transient",
			err:      awserr.New("ValidationError", "Invalid parameter", nil),
			expected: false,
		},
		{
			name:     "message indicates transient state",
			err:      awserr.New("SomeError", "resource is being updated, try again", nil),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTransientAWSError(tt.err)
			if result != tt.expected {
				t.Errorf("isTransientAWSError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}
