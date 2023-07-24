package aws

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
)

const (
	// ProviderName is the name of the provider
	ProviderName = "aws"

	validationErrorCode     = "ValidationError"
	alreadyAttachedMessage  = "is already part of AutoScalingGroup"
	alreadyDetachingMessage = "is not in InService or Standby"

	// launchTemplateLatestVersion defines the launching of the latest version of the template.
	launchTemplateLatestVersion = "$Latest"
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
	if instanceID == "" || availabilityZone == "" {
		return "", fmt.Errorf("instanceID and availabilityZone cannot be empty")
	}
	return fmt.Sprintf("aws:///%s/%s", availabilityZone, instanceID), nil
}

func verifyIfErrorOccurred(apiErr error, expectedMessage ...string) (bool, error) {
	for _, msg := range expectedMessage {
		if awsErr, ok := apiErr.(awserr.Error); ok {
			// process SDK error: Unfortunately there's no generic ValidationError in the SDK and no FailedAttach/FailedDetach error. Check manually
			if awsErr.Code() == validationErrorCode && strings.Contains(awsErr.Message(), msg) {
				return true, apiErr
			}
		}
	}
	return false, apiErr
}

func verifyIfErrorOccurredWithDefaults(apiErr error, expectedMessage string) (bool, error) {
	skip_errs := []string{
		// default errors we wanted to skip
		"is not in correct state",
		expectedMessage,
	}
	return verifyIfErrorOccurred(apiErr, skip_errs...)
}

type provider struct {
	autoScalingService *autoscaling.AutoScaling
	ec2Service         *ec2.EC2
	logger             logr.Logger
}

type autoscalingGroups struct {
	autoScalingService autoscalingiface.AutoScalingAPI
	ec2Service         ec2iface.EC2API
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
		ec2Service:         p.ec2Service,
		logger:             p.logger,
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
			if providerID, ok := instanceIDSet[aws.StringValue(instance.InstanceId)]; ok {
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
			providerID, err := instanceIDToProviderID(aws.StringValue(i.InstanceId), aws.StringValue(i.AvailabilityZone))
			if err != nil {
				a.logger.Info("[Instances] skip instance which failed instanceID to providerID conversion: %v", aws.StringValue(i.InstanceId))
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
			providerID, err := instanceIDToProviderID(aws.StringValue(i.InstanceId), *i.AvailabilityZone)
			if err != nil {
				a.logger.Info("[ReadyInstances] skip instance which failed instanceID to providerID conversion: %v", aws.StringValue(i.InstanceId))
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
				providerID, err := instanceIDToProviderID(aws.StringValue(i.InstanceId), aws.StringValue(i.AvailabilityZone))
				if err != nil {
					a.logger.Info("[NotReadyInstances] skip instance which failed instanceID to providerID conversion: %v", aws.StringValue(i.InstanceId))
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
			if aws.StringValue(i.InstanceId) == instanceID {
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
	return nil, fmt.Errorf("failed to find target node group for instance with ID: %v", instanceID)
}

// getInstanceNodeGroupByGroupName finds the right autoscaling group for an instance based on
// nodeGroup name passed in
func (a *autoscalingGroups) getInstanceNodeGroupByGroupName(nodeGroup string) (*autoscaling.Group, error) {
	if nodeGroup == "" {
		return nil, fmt.Errorf("nodeGroup is empty")
	}

	for _, group := range a.groups {
		if *group.AutoScalingGroupName == nodeGroup {
			return group, nil
		}
	}
	return nil, fmt.Errorf("failed to find target node group: %v", nodeGroup)
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

	return verifyIfErrorOccurred(apiErr, alreadyDetachingMessage)
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

	return verifyIfErrorOccurredWithDefaults(apiErr, alreadyAttachedMessage)
}

func (a *autoscalingGroups) instanceOutOfDate(instance *autoscaling.Instance) bool {
	group, err := a.getInstanceNodeGroupByInstanceID(aws.StringValue(instance.InstanceId))
	if err != nil {
		return false
	}
	var groupVersion string
	switch {
	case group.LaunchConfigurationName != nil:
		groupVersion = aws.StringValue(group.LaunchConfigurationName)
	case group.LaunchTemplate != nil:
		groupVersion = aws.StringValue(group.LaunchTemplate.Version)

		if groupVersion == launchTemplateLatestVersion {
			groupVersion, err = a.getLaunchTemplateLatestVersion(aws.StringValue(group.LaunchTemplate.LaunchTemplateId))
			if err != nil {
				a.logger.WithValues(
					"lt-id", aws.StringValue(group.LaunchTemplate.LaunchTemplateId),
					"lt-name", aws.StringValue(group.LaunchTemplate.LaunchTemplateName),
				).Error(err, "[ASG] failed to get latest asg version")
				return false
			}
		}

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

	a.logger.WithValues(
		"instance", instanceVersion,
		"asg", groupVersion,
		"asg-name", aws.StringValue(group.AutoScalingGroupName),
	).Info("[ASG] out of date version check")

	return groupVersion != instanceVersion
}

func (a *autoscalingGroups) getLaunchTemplateLatestVersion(id string) (string, error) {
	input := &ec2.DescribeLaunchTemplateVersionsInput{
		LaunchTemplateId: aws.String(id),
		Versions:         aws.StringSlice([]string{launchTemplateLatestVersion}),
	}
	out, err := a.ec2Service.DescribeLaunchTemplateVersions(input)
	if err != nil {
		return "", err
	}

	if len(out.LaunchTemplateVersions) == 0 {
		return "", errors.Wrapf(err, "[ASG ]failed to get latest launch template version %q", id)
	}

	return strconv.Itoa(int(*out.LaunchTemplateVersions[0].VersionNumber)), nil
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
		return aws.StringValue(i.instance.InstanceId) == instanceID
	}
	return false
}

// NodeGroupName returns cloud provider node group name for the instance
func (i *instance) NodeGroupName() string {
	return i.nodeGroupName
}
