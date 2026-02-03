package transitioner

import (
	"net"
	"strings"

	"github.com/aws/aws-sdk-go/aws/awserr"
)

// isRetryableError determines if an error should trigger a requeue instead of moving to Healing phase
// Retryable errors typically include network timeouts and transient AWS service errors
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for network errors (timeouts, connection refused, etc.)
	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() || netErr.Temporary() {
			return true
		}
	}

	// Check error string for common network patterns
	errStr := err.Error()
	networkPatterns := []string{
		"i/o timeout",
		"dial tcp",
		"connection reset",
		"connection refused",
		"connection timeout",
		"TLS handshake timeout",
		"broken pipe",
		"unexpected EOF",
	}

	for _, pattern := range networkPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	// Check for AWS SDK transient errors
	if awsErr, ok := err.(awserr.Error); ok {
		code := awsErr.Code()
		// Common transient AWS error codes
		transientCodes := map[string]bool{
			"Throttling":                  true,
			"ThrottlingException":         true,
			"TooManyRequestsException":    true,
			"RequestLimitExceeded":        true,
			"SlowDown":                    true,
			"ServiceUnavailable":          true,
			"ServiceUnavailableException": true,
			"InternalFailure":             true,
			"InternalError":               true,
			"InternalServerError":         true,
			"RequestTimeout":              true,
			"RequestTimedOut":             true,
		}

		if transientCodes[code] {
			return true
		}
	}

	return false
}
