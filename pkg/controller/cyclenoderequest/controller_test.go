package cyclenoderequest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckAPIVersionCompatibility covers every branch of checkAPIVersionCompatibility.
func TestCheckAPIVersionCompatibility(t *testing.T) {
	tests := []struct {
		name                string
		cnrAnnotation       string
		controllerVersion   string
		wantSkip            bool
		wantFailed          bool
		wantFailMsgContains string
		wantErr             bool
	}{
		// No client annotation 
		{
			name:              "empty annotation skips check",
			cnrAnnotation:     "",
			controllerVersion: "1.2.0",
			wantSkip:          true,
			wantFailed:        false,
		},
		{
			name:              "annotation value 'undefined' skips check",
			cnrAnnotation:     "undefined",
			controllerVersion: "1.2.0",
			wantSkip:          true,
			wantFailed:        false,
		},

		// Controller version not injected via ldflags
		{
			name:              "controller version undefined skips check and continues reconciliation",
			cnrAnnotation:     "1.0.0",
			controllerVersion: "undefined",
			wantSkip:          true,
			wantFailed:        false,
		},

		// Version comparison
		{
			name:              "client version equal to controller version is compatible",
			cnrAnnotation:     "1.2.0",
			controllerVersion: "1.2.0",
			wantSkip:          false,
			wantFailed:        false,
		},
		{
			name:              "client version newer than controller version is compatible",
			cnrAnnotation:     "1.3.0",
			controllerVersion: "1.2.0",
			wantSkip:          false,
			wantFailed:        false,
		},
		{
			name:                "client version older than controller version fails the CNR",
			cnrAnnotation:       "1.1.0",
			controllerVersion:   "1.2.0",
			wantSkip:            false,
			wantFailed:          true,
			wantFailMsgContains: "1.1.0",
		},
		{
			name:                "client version significantly older than controller version fails",
			cnrAnnotation:       "0.9.0",
			controllerVersion:   "2.0.0",
			wantSkip:            false,
			wantFailed:          true,
			wantFailMsgContains: "0.9.0",
		},
		{
			name:              "client patch version newer is compatible",
			cnrAnnotation:     "1.2.3",
			controllerVersion: "1.2.2",
			wantSkip:          false,
			wantFailed:        false,
		},
		{
			name:                "client patch version older fails",
			cnrAnnotation:       "1.2.1",
			controllerVersion:   "1.2.2",
			wantSkip:            false,
			wantFailed:          true,
			wantFailMsgContains: "1.2.1",
		},

		// Malformed versions
		{
			name:              "malformed controller version returns error",
			cnrAnnotation:     "1.0.0",
			controllerVersion: "not-a-version",
			wantErr:           true,
		},
		{
			name:              "malformed client annotation returns error",
			cnrAnnotation:     "not-a-version",
			controllerVersion: "1.0.0",
			wantErr:           true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := checkAPIVersionCompatibility(tc.cnrAnnotation, tc.controllerVersion)

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantSkip, result.skipCheck, "skipCheck mismatch")
			assert.Equal(t, tc.wantFailed, result.failed, "failed mismatch")

			if tc.wantFailMsgContains != "" {
				assert.Contains(t, result.failMsg, tc.wantFailMsgContains)
				// Ensure the controller version is also surfaced in the message
				assert.Contains(t, result.failMsg, tc.controllerVersion)
			} else {
				assert.Empty(t, result.failMsg)
			}
		})
	}
}
