package builder

import (
	"fmt"

	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider/aws"
	"github.com/go-logr/logr"
)

type builderFunc func(logger logr.Logger) (cloudprovider.CloudProvider, error)

// BuildCloudProvider returns a cloud provider based on the provided name with default retry configuration
func BuildCloudProvider(name string, logger logr.Logger) (cloudprovider.CloudProvider, error) {
	buildFuncs := map[string]builderFunc{
		aws.ProviderName: aws.NewCloudProvider,
	}

	builder, ok := buildFuncs[name]
	if !ok {
		return nil, fmt.Errorf("builder for cloud provider %v not found", name)
	}

	return builder(logger)
}

// BuildCloudProviderWithRetryConfig returns a cloud provider with custom retry configuration
func BuildCloudProviderWithRetryConfig(name string, logger logr.Logger, retryEnabled bool, maxRetries, initialDelayMs, maxDelayMs int) (cloudprovider.CloudProvider, error) {
	switch name {
	case aws.ProviderName:
		retryConfig := aws.NewRetryConfig(retryEnabled, maxRetries, initialDelayMs, maxDelayMs)
		return aws.NewCloudProviderWithRetryConfig(logger, retryConfig)
	default:
		return nil, fmt.Errorf("builder for cloud provider %v not found", name)
	}
}
