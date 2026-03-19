package aws

import (
	"fmt"

	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	fakeaws "github.com/atlassian-labs/cyclops/pkg/cloudprovider/aws/fake"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
)

// NewCloudProvider returns a new AWS cloud provider using the AWS SDK's default retry behavior
func NewCloudProvider(logger logr.Logger) (cloudprovider.CloudProvider, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	// Configure AWS SDK with default retry logic
	// The AWS SDK v1 automatically uses client.DefaultRetryer which handles:
	// - Exponential backoff
	// - Retries for throttling errors
	// - Retries for transient network errors
	// - Retries for 5xx server errors
	// AWS SDK uses its default credential chain (env vars → shared credentials file → IAM instance profile / IRSA).
	config := &aws.Config{
		// Use AWS SDK's default retry behavior (3 retries with exponential backoff)
		// This is sufficient for most use cases
		MaxRetries: aws.Int(3),
	}

	ec2Service := ec2.New(sess, config)
	autoScalingService := autoscaling.New(sess, config)

	p := &provider{
		autoScalingService: autoScalingService,
		ec2Service:         ec2Service,
		logger:             logger,
	}

	// Log the provider we used. Credentials are resolved by the session's
	// default chain, so read them from the session rather than the config.
	credValue, err := sess.Config.Credentials.Get()
	if err != nil {
		return nil, err
	}
	logger.Info(fmt.Sprintf("aws session created successfully, using provider %v", credValue.ProviderName))

	return p, nil
}

// NewGenericCloudProvider returns a new mock AWS cloud provider
func NewGenericCloudProvider(autoscalingiface *fakeaws.Autoscaling, ec2iface *fakeaws.Ec2) cloudprovider.CloudProvider {
	return &provider{
		autoScalingService: autoscalingiface,
		ec2Service:         ec2iface,
	}
}
