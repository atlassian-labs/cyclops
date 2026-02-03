package aws

import (
	"testing"
	"time"
)

func TestRetryConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    RetryConfig
		wantError bool
		errMsg    string
	}{
		{
			name:      "disabled config requires no validation",
			config:    DisabledRetryConfig(),
			wantError: false,
		},
		{
			name: "valid enabled config",
			config: RetryConfig{
				Enabled:        true,
				MaxRetries:     5,
				InitialDelayMs: 100,
				MaxDelayMs:     30000,
			},
			wantError: false,
		},
		{
			name: "zero max retries",
			config: RetryConfig{
				Enabled:        true,
				MaxRetries:     0,
				InitialDelayMs: 100,
				MaxDelayMs:     30000,
			},
			wantError: true,
			errMsg:    "MaxRetries must be at least 1",
		},
		{
			name: "negative initial delay",
			config: RetryConfig{
				Enabled:        true,
				MaxRetries:     5,
				InitialDelayMs: -100,
				MaxDelayMs:     30000,
			},
			wantError: true,
			errMsg:    "InitialDelayMs cannot be negative",
		},
		{
			name: "max delay less than initial delay",
			config: RetryConfig{
				Enabled:        true,
				MaxRetries:     5,
				InitialDelayMs: 1000,
				MaxDelayMs:     100,
			},
			wantError: true,
			errMsg:    "MaxDelayMs must be >= InitialDelayMs",
		},
		{
			name: "single retry",
			config: RetryConfig{
				Enabled:        true,
				MaxRetries:     1,
				InitialDelayMs: 100,
				MaxDelayMs:     100,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
			if tt.wantError && err != nil && tt.errMsg != "" {
				if err.Error() != tt.errMsg {
					t.Errorf("Validate() error message = %v, want %v", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestRetryConfigGetInitialDelay(t *testing.T) {
	tests := []struct {
		name     string
		config   RetryConfig
		expected time.Duration
	}{
		{
			name:     "100ms initial delay",
			config:   RetryConfig{InitialDelayMs: 100},
			expected: 100 * time.Millisecond,
		},
		{
			name:     "1000ms initial delay",
			config:   RetryConfig{InitialDelayMs: 1000},
			expected: 1000 * time.Millisecond,
		},
		{
			name:     "zero initial delay",
			config:   RetryConfig{InitialDelayMs: 0},
			expected: 0 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetInitialDelay()
			if got != tt.expected {
				t.Errorf("GetInitialDelay() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRetryConfigGetMaxDelay(t *testing.T) {
	tests := []struct {
		name     string
		config   RetryConfig
		expected time.Duration
	}{
		{
			name:     "30000ms max delay",
			config:   RetryConfig{MaxDelayMs: 30000},
			expected: 30000 * time.Millisecond,
		},
		{
			name:     "5000ms max delay",
			config:   RetryConfig{MaxDelayMs: 5000},
			expected: 5000 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetMaxDelay()
			if got != tt.expected {
				t.Errorf("GetMaxDelay() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if !config.Enabled {
		t.Error("DefaultRetryConfig should have Enabled=true")
	}

	if config.MaxRetries != 5 {
		t.Errorf("DefaultRetryConfig MaxRetries = %v, want 5", config.MaxRetries)
	}

	if config.InitialDelayMs != 5000 {
		t.Errorf("DefaultRetryConfig InitialDelayMs = %v, want 5000", config.InitialDelayMs)
	}

	if config.MaxDelayMs != 60000 {
		t.Errorf("DefaultRetryConfig MaxDelayMs = %v, want 60000", config.MaxDelayMs)
	}

	if err := config.Validate(); err != nil {
		t.Errorf("DefaultRetryConfig validation failed: %v", err)
	}
}

func TestDisabledRetryConfig(t *testing.T) {
	config := DisabledRetryConfig()

	if config.Enabled {
		t.Error("DisabledRetryConfig should have Enabled=false")
	}

	if err := config.Validate(); err != nil {
		t.Errorf("DisabledRetryConfig validation failed: %v", err)
	}
}

func TestNewRetryConfig(t *testing.T) {
	config := NewRetryConfig(true, 3, 50, 5000)

	if !config.Enabled {
		t.Error("NewRetryConfig should have Enabled=true")
	}

	if config.MaxRetries != 3 {
		t.Errorf("NewRetryConfig MaxRetries = %v, want 3", config.MaxRetries)
	}

	if config.InitialDelayMs != 50 {
		t.Errorf("NewRetryConfig InitialDelayMs = %v, want 50", config.InitialDelayMs)
	}

	if config.MaxDelayMs != 5000 {
		t.Errorf("NewRetryConfig MaxDelayMs = %v, want 5000", config.MaxDelayMs)
	}

	if err := config.Validate(); err != nil {
		t.Errorf("NewRetryConfig validation failed: %v", err)
	}
}
