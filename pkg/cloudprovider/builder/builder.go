package builder

import (
	"fmt"

	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider/aws"
	"github.com/go-logr/logr"
)

type builderFunc func(logger logr.Logger) (cloudprovider.CloudProvider, error)

// BuildCloudProvider returns a cloud provider based on the provided name
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
