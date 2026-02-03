package aws

import (
	"fmt"
	"time"

	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	fakeaws "github.com/atlassian-labs/cyclops/pkg/cloudprovider/aws/fake"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
)

// customRetryer wraps the default SDK retryer with custom retry logic
type customRetryer struct {
	client.DefaultRetryer
}

// ShouldRetry determines if a request should be retried based on the error
func (r customRetryer) ShouldRetry(req *request.Request) bool {
	// Always retry on connection errors, timeouts, and other transient failures
	if req.IsErrorRetryable() {
		return true
	}

	// Check for specific error conditions that should be retried
	if req.Error != nil {
		if isTransientError(req.Error) {
			return true
		}
	}

	// Fall back to default retry logic
	return r.DefaultRetryer.ShouldRetry(req)
}

// NewCloudProvider returns a new AWS cloud provider with default retry configuration
func NewCloudProvider(logger logr.Logger) (cloudprovider.CloudProvider, error) {
	return NewCloudProviderWithRetryConfig(logger, DefaultRetryConfig())
}

// NewCloudProviderWithRetryConfig returns a new AWS cloud provider with custom retry configuration
func NewCloudProviderWithRetryConfig(logger logr.Logger, retryConfig RetryConfig) (cloudprovider.CloudProvider, error) {
	// Validate retry configuration
	if err := retryConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid retry configuration: %w", err)
	}

	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	var creds *credentials.Credentials

	// Configure AWS SDK with retry logic and timeouts
	config := &aws.Config{
		Credentials: creds,
		// Maximum number of retries for API calls
		MaxRetries: aws.Int(5),
		// Custom retryer with exponential backoff
		Retryer: customRetryer{
			DefaultRetryer: client.DefaultRetryer{
				NumMaxRetries:    5,
				MinRetryDelay:    1 * time.Second,
				MaxRetryDelay:    30 * time.Second,
				MinThrottleDelay: 500 * time.Millisecond,
			},
		},
		// HTTP client timeout (increased from default to handle slow networks)
		HTTPClient: &aws.HTTPClient{
			Timeout: 30 * time.Second,
		},
	}

	ec2Service := ec2.New(sess, config)
	autoScalingService := autoscaling.New(sess, config)

	p := &provider{
		autoScalingService: autoScalingService,
		ec2Service:         ec2Service,
		logger:             logger,
		retryConfig:        retryConfig,
	}

	// Log the provider we used
	credValue, err := autoScalingService.Client.Config.Credentials.Get()
	if err != nil {
		return nil, err
	}
	logger.Info(fmt.Sprintf("aws session created successfully, using provider %v", credValue.ProviderName))
	logger.Info("AWS retry configuration",
		"enabled", retryConfig.Enabled,
		"maxRetries", retryConfig.MaxRetries,
		"initialDelayMs", retryConfig.InitialDelayMs,
		"maxDelayMs", retryConfig.MaxDelayMs)

	return p, nil
}

// NewGenericCloudProvider returns a new mock AWS cloud provider
func NewGenericCloudProvider(autoscalingiface *fakeaws.Autoscaling, ec2iface *fakeaws.Ec2) cloudprovider.CloudProvider {
	return &provider{
		autoScalingService: autoscalingiface,
		ec2Service:         ec2iface,
	}
}
