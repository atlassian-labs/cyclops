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

func providerIDToInstanceID(providerID string) (instanceID string, err error) {
	res := providerIDRegex.FindStringSubmatch(providerID)
	if len(res) != 2 {
		return "", fmt.Errorf("unable to extract instance ID from provider ID")
	}
	return res[1], nil
}

func instanceIDToProviderID(instanceID, availabilityZone string) (string, error) {
	if len(instanceID) == 0 || len(availabilityZone) == 0 {
		return "", fmt.Errorf("instanceID and availabilityZone cannot be empty")
	}
	return fmt.Sprintf("aws:///%s/%s", availabilityZone, instanceID), nil
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

type autoscalingGroups struct {
	autoScalingService *autoscaling.AutoScaling
	groups             []*autoscaling.Group
	logger             logr.Logger
}

type instance struct {
	instance      *autoscaling.Instance
	nodeGroupName string
	outOfDate     bool
}

// Name returns the name of the cloud provider
func (p *provider) Name() string {
	return ProviderName
}

// GetNodeGroups gets a Autoscaling groups
func (p *provider) GetNodeGroups(names []string) (cloudprovider.NodeGroups, error) {
	result, err := p.autoScalingService.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice(names),
	})
	if err != nil {
		return nil, err
	}

	groups := result.AutoScalingGroups
	if len(groups) == 0 {
		return nil, fmt.Errorf("autoscaling groups not found: %v", names)
	}

	return &autoscalingGroups{
		groups:             groups,
		autoScalingService: p.autoScalingService,
	}, nil
}

// InstancesExist returns a list of the instances that exist
func (p *provider) InstancesExist(providerIDs []string) (validProviderIDs []string, err error) {
	instanceIDSet := map[string]string{}
	instanceIDs := []string{}

	for _, providerID := range providerIDs {
		instanceID, err := providerIDToInstanceID(providerID)
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
	instanceID, err := providerIDToInstanceID(providerID)
	if err != nil {
		return err
	}

	_, err = p.ec2Service.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
	return err
}

// Instances returns a map of all instances in the Autoscaling group
// with providerID as key and cloudprovider.Instance as value
func (a *autoscalingGroups) Instances() map[string]cloudprovider.Instance {
	instances := make(map[string]cloudprovider.Instance)
	for _, group := range a.groups {
		for _, i := range group.Instances {
			providerID, err := instanceIDToProviderID(*i.InstanceId, *i.AvailabilityZone)
			if err != nil {
				a.logger.Info("[Instances] skip instance which failed instanceID to providerID conversion: %v", *i.InstanceId)
				continue
			}
			instances[providerID] = &instance{
				instance:      i,
				outOfDate:     a.instanceOutOfDate(i),
				nodeGroupName: *group.AutoScalingGroupName,
			}
		}
	}

	return instances
}

// ReadyInstances returns a map of instances that are InService
// with providerID as key and cloudprovider.Instance as value
func (a *autoscalingGroups) ReadyInstances() map[string]cloudprovider.Instance {
	instances := make(map[string]cloudprovider.Instance)
	for _, group := range a.groups {
		for _, i := range group.Instances {
			if aws.StringValue(i.LifecycleState) != "InService" {
				continue
			}
			providerID, err := instanceIDToProviderID(*i.InstanceId, *i.AvailabilityZone)
			if err != nil {
				a.logger.Info("[ReadyInstances] skip instance which failed instanceID to providerID conversion: %v", *i.InstanceId)
				continue
			}
			instances[providerID] = &instance{
				instance:      i,
				outOfDate:     a.instanceOutOfDate(i),
				nodeGroupName: *group.AutoScalingGroupName,
			}
		}
	}
	return instances
}

// NotReadyInstances returns a map of instances that are not InService
// with providerID as key and cloudprovider.Instance as value
func (a *autoscalingGroups) NotReadyInstances() map[string]cloudprovider.Instance {
	instances := make(map[string]cloudprovider.Instance)
	for _, group := range a.groups {
		for _, i := range group.Instances {
			if aws.StringValue(i.LifecycleState) != "InService" {
				providerID, err := instanceIDToProviderID(*i.InstanceId, *i.AvailabilityZone)
				if err != nil {
					a.logger.Info("[NotReadyInstances] skip instance which failed instanceID to providerID conversion: %v", *i.InstanceId)
					continue
				}
				instances[providerID] = &instance{
					instance:      i,
					outOfDate:     a.instanceOutOfDate(i),
					nodeGroupName: *group.AutoScalingGroupName,
				}
			}
		}
	}
	return instances
}

// getInstanceNodeGroupByInstanceID finds the right autoscaling group for an instance based on
// instance ID matches one instance ID from one of the node groups inside nodeGroupsList
func (a *autoscalingGroups) getInstanceNodeGroupByInstanceID(instanceID string) (*autoscaling.Group, error) {
	var targetNodeGroup *autoscaling.Group
	foundNodeGroup := false
	for _, group := range a.groups {
		for _, i := range group.Instances {
			if *i.InstanceId == instanceID {
				targetNodeGroup = group
				foundNodeGroup = true
				break
			}
		}
		if foundNodeGroup {
			break
		}
	}
	if foundNodeGroup {
		return targetNodeGroup, nil
	}
	return nil, fmt.Errorf("failed to found target node group for instance with ID: %v", instanceID)
}

// getInstanceNodeGroupByGroupName finds the right autoscaling group for an instance based on
// nodeGroup name passed in
func (a *autoscalingGroups) getInstanceNodeGroupByGroupName(nodeGroup string) (*autoscaling.Group, error) {
	if nodeGroup != "" {
		for _, group := range a.groups {
			if *group.AutoScalingGroupName == nodeGroup {
				return group, nil
			}
		}
	}
	return nil, fmt.Errorf("failed to found target node group for instance with group: %v", nodeGroup)
}

// DetachInstance detaches the instance from the Autoscaling group
func (a *autoscalingGroups) DetachInstance(providerID string) (alreadyDetaching bool, err error) {
	instanceID, err := providerIDToInstanceID(providerID)
	if err != nil {
		return false, err
	}

	group, err := a.getInstanceNodeGroupByInstanceID(instanceID)
	if err != nil {
		return false, err
	}

	_, apiErr := a.autoScalingService.DetachInstances(&autoscaling.DetachInstancesInput{
		AutoScalingGroupName:           group.AutoScalingGroupName,
		InstanceIds:                    aws.StringSlice([]string{instanceID}),
		ShouldDecrementDesiredCapacity: aws.Bool(false),
	})

	return verifyIfErrorOccured(apiErr, alreadyDetachingMessage)
}

// AttachInstance attaches the instance to the Autoscaling group
func (a *autoscalingGroups) AttachInstance(providerID, nodeGroup string) (alreadyAttached bool, err error) {
	instanceID, err := providerIDToInstanceID(providerID)
	if err != nil {
		return false, err
	}

	group, err := a.getInstanceNodeGroupByGroupName(nodeGroup)
	if err != nil {
		return false, err
	}

	_, apiErr := a.autoScalingService.AttachInstances(&autoscaling.AttachInstancesInput{
		AutoScalingGroupName: group.AutoScalingGroupName,
		InstanceIds:          aws.StringSlice([]string{instanceID}),
	})

	return verifyIfErrorOccured(apiErr, alreadyAttachedMessage)
}

func (a *autoscalingGroups) instanceOutOfDate(instance *autoscaling.Instance) bool {
	group, err := a.getInstanceNodeGroupByInstanceID(*instance.InstanceId)
	if err != nil {
		return false
	}
	var groupVersion string
	switch {
	case group.LaunchConfigurationName != nil:
		groupVersion = aws.StringValue(group.LaunchConfigurationName)
	case group.LaunchTemplate != nil:
		groupVersion = aws.StringValue(group.LaunchTemplate.Version)
	case group.MixedInstancesPolicy != nil:
		if policy := group.MixedInstancesPolicy; policy.LaunchTemplate != nil && policy.LaunchTemplate.LaunchTemplateSpecification != nil {
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
	if instanceID, err := providerIDToInstanceID(providerID); err == nil {
		return *i.instance.InstanceId == instanceID
	}
	return false
}

// NodeGroupName returns cloud provider node group name for the instance
func (i *instance) NodeGroupName() string {
	return i.nodeGroupName
}
