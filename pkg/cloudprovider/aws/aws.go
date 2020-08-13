package aws

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-logr/logr"
)

const (
	// ProviderName is the name of the provider
	ProviderName = "aws"

	validationErrorCode     = "ValidationError"
	alreadyAttachedMessage  = "is already part of AutoScalingGroup"
	alreadyDetachingMessage = "is not in InService or Standby"
)

var providerIDRegex = regexp.MustCompile(`aws:\/\/\/[\w-]+\/([\w-]+)`)

func convertProviderID(providerID string) (instanceID string, err error) {
	res := providerIDRegex.FindStringSubmatch(providerID)
	if len(res) != 2 {
		return "", fmt.Errorf("unable to extract instance ID from provider ID")
	}
	return res[1], nil
}

func verifyIfErrorOccured(apiErr error, expectedMessage string) (bool, error) {
	if awsErr, ok := apiErr.(awserr.Error); ok {
		// process SDK error: Unfortunately there's no generic ValidationError in the SDK and no FailedAttach/FailedDetach error. Check manually
		if awsErr.Code() == validationErrorCode && strings.Contains(awsErr.Message(), expectedMessage) {
			return true, apiErr
		}
	}

	return false, apiErr
}

type provider struct {
	autoScalingService *autoscaling.AutoScaling
	ec2Service         *ec2.EC2
	logger             logr.Logger
}

type autoscalingGroup struct {
	autoScalingService *autoscaling.AutoScaling
	group              *autoscaling.Group
}

type instance struct {
	instance  *autoscaling.Instance
	outOfDate bool
}

// Name returns the name of the cloud provider
func (p *provider) Name() string {
	return ProviderName
}

// GetNodeGroup gets a Autoscaling group
func (p *provider) GetNodeGroup(name string) (cloudprovider.NodeGroup, error) {
	result, err := p.autoScalingService.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{name}),
	})
	if err != nil {
		return nil, err
	}

	groups := result.AutoScalingGroups
	if len(groups) == 0 {
		return nil, fmt.Errorf("autoscaling group %v not found", name)
	}

	return &autoscalingGroup{
		group:              groups[0],
		autoScalingService: p.autoScalingService,
	}, nil
}

// InstancesExist returns a list of the instances that exist
func (p *provider) InstancesExist(providerIDs []string) (validProviderIDs []string, err error) {
	instanceIDSet := map[string]string{}
	instanceIDs := []string{}

	for _, providerID := range providerIDs {
		instanceID, err := convertProviderID(providerID)
		if err != nil {
			return nil, err
		}

		instanceIDSet[instanceID] = providerID
		instanceIDs = append(instanceIDs, instanceID)
	}

	output, err := p.ec2Service.DescribeInstances(
		&ec2.DescribeInstancesInput{
			InstanceIds: aws.StringSlice(instanceIDs),
		},
	)

	if err != nil {
		return nil, err
	}

	for _, reservation := range output.Reservations {
		for _, instance := range reservation.Instances {
			if providerID, ok := instanceIDSet[*instance.InstanceId]; ok {
				validProviderIDs = append(validProviderIDs, providerID)
			}
		}
	}

	return validProviderIDs, nil
}

// TerminateInstance terminates an instance
func (p *provider) TerminateInstance(providerID string) error {
	instanceID, err := convertProviderID(providerID)
	if err != nil {
		return err
	}

	_, err = p.ec2Service.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
	return err
}

// ID returns the ID for the Autoscaling group
func (a *autoscalingGroup) ID() string {
	return aws.StringValue(a.group.AutoScalingGroupName)
}

// Instances returns a slice of all instances in the Autoscaling group
func (a *autoscalingGroup) Instances() (instances []cloudprovider.Instance) {
	for _, i := range a.group.Instances {
		instances = append(instances, &instance{
			instance:  i,
			outOfDate: a.instanceOutOfDate(i),
		})
	}
	return instances
}

// ReadyInstances returns a slice of instances that are InService
func (a *autoscalingGroup) ReadyInstances() (instances []cloudprovider.Instance) {
	for _, i := range a.group.Instances {
		if aws.StringValue(i.LifecycleState) != "InService" {
			continue
		}
		instances = append(instances, &instance{
			instance:  i,
			outOfDate: a.instanceOutOfDate(i),
		})
	}
	return instances
}

// NotReadyInstances returns a slice of instances that are not InService
func (a *autoscalingGroup) NotReadyInstances() (instances []cloudprovider.Instance) {
	for _, i := range a.group.Instances {
		if aws.StringValue(i.LifecycleState) != "InService" {
			instances = append(instances, &instance{
				instance:  i,
				outOfDate: a.instanceOutOfDate(i),
			})
		}
	}
	return instances
}

// DetachInstance detaches the instance from the Autoscaling group
func (a *autoscalingGroup) DetachInstance(providerID string) (alreadyDetaching bool, err error) {
	instanceID, err := convertProviderID(providerID)
	if err != nil {
		return false, err
	}

	_, apiErr := a.autoScalingService.DetachInstances(&autoscaling.DetachInstancesInput{
		AutoScalingGroupName:           a.group.AutoScalingGroupName,
		InstanceIds:                    aws.StringSlice([]string{instanceID}),
		ShouldDecrementDesiredCapacity: aws.Bool(false),
	})

	return verifyIfErrorOccured(apiErr, alreadyDetachingMessage)
}

// AttachInstances attaches the instance to the Autoscaling group
func (a *autoscalingGroup) AttachInstance(providerID string) (alreadyAttached bool, err error) {
	instanceID, err := convertProviderID(providerID)
	if err != nil {
		return false, err
	}

	_, apiErr := a.autoScalingService.AttachInstances(&autoscaling.AttachInstancesInput{
		AutoScalingGroupName: a.group.AutoScalingGroupName,
		InstanceIds:          aws.StringSlice([]string{instanceID}),
	})

	return verifyIfErrorOccured(apiErr, alreadyAttachedMessage)
}

func (a *autoscalingGroup) instanceOutOfDate(instance *autoscaling.Instance) bool {
	var groupVersion string
	switch {
	case a.group.LaunchConfigurationName != nil:
		groupVersion = aws.StringValue(a.group.LaunchConfigurationName)
	case a.group.LaunchTemplate != nil:
		groupVersion = aws.StringValue(a.group.LaunchTemplate.Version)
	case a.group.MixedInstancesPolicy != nil:
		if policy := a.group.MixedInstancesPolicy; policy.LaunchTemplate != nil && policy.LaunchTemplate.LaunchTemplateSpecification != nil {
			groupVersion = aws.StringValue(policy.LaunchTemplate.LaunchTemplateSpecification.Version)
		}
	}

	var instanceVersion string
	switch {
	case instance.LaunchConfigurationName != nil:
		instanceVersion = aws.StringValue(instance.LaunchConfigurationName)
	case instance.LaunchTemplate != nil:
		instanceVersion = aws.StringValue(instance.LaunchTemplate.Version)
	}

	return groupVersion != instanceVersion
}

// ID returns the ID for the instance
func (i *instance) ID() string {
	return aws.StringValue(i.instance.InstanceId)
}

// String returns the instance ID for the instance
func (i *instance) String() string {
	return i.ID()
}

// OutOfDate returns if the instance is out of date from it's attached node group
func (i *instance) OutOfDate() bool {
	return i.outOfDate
}

// MatchesProviderID returns if the instance ID matches the providerID
func (i *instance) MatchesProviderID(providerID string) bool {
	if instanceID, err := convertProviderID(providerID); err == nil {
		return *i.instance.InstanceId == instanceID
	}
	return false
}
