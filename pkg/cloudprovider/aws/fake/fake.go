package fakeaws

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
)

var (
	defaultAvailabilityZone = "us-east-1a"
)

type Instance struct {
	InstanceID           string
	AutoscalingGroupName string
	State                string
}

type Ec2 struct {
	ec2iface.EC2API

	Instances map[string]*Instance
}

type Autoscaling struct {
	autoscalingiface.AutoScalingAPI

	Instances map[string]*Instance
}

func GenerateProviderID(instanceID string) string {
	return fmt.Sprintf("aws:///%s/%s",
		defaultAvailabilityZone,
		instanceID,
	)
}

func generateEc2Instance(instance *Instance) *ec2.Instance {
	ec2Instance := &ec2.Instance{
		InstanceId: aws.String(instance.InstanceID),
		State: &ec2.InstanceState{
			Name: aws.String(instance.State),
		},
	}

	return ec2Instance
}

func generateAutoscalingInstance(instance *Instance) *autoscaling.Instance {
	autoscalingInstance := &autoscaling.Instance{
		InstanceId:       aws.String(instance.InstanceID),
		AvailabilityZone: aws.String(defaultAvailabilityZone),
	}

	return autoscalingInstance
}

// *************** Autoscaling *************** //

func (m *Autoscaling) DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	var asgs = make(map[string]*autoscaling.Group, 0)

	var asgNameLookup = make(map[string]interface{})

	for _, asgName := range input.AutoScalingGroupNames {
		asgNameLookup[*asgName] = nil
	}

	for _, instance := range m.Instances {
		if instance.AutoscalingGroupName == "" {
			continue
		}

		if _, exists := asgNameLookup[instance.AutoscalingGroupName]; !exists {
			continue
		}

		asg, exists := asgs[instance.AutoscalingGroupName]

		if !exists {
			asg = &autoscaling.Group{
				AutoScalingGroupName: aws.String(instance.AutoscalingGroupName),
				Instances:            []*autoscaling.Instance{},
				AvailabilityZones: []*string{
					aws.String(defaultAvailabilityZone),
				},
			}

			asgs[instance.AutoscalingGroupName] = asg
		}

		asg.Instances = append(
			asg.Instances,
			generateAutoscalingInstance(instance),
		)
	}

	var asgList = make([]*autoscaling.Group, 0)

	for _, asg := range asgs {
		asgList = append(asgList, asg)
	}

	return &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: asgList,
	}, nil
}

// *************** EC2 *************** //

func (m *Ec2) DescribeInstances(input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	var instances = make([]*ec2.Instance, 0)
	var instanceIds = make(map[string]interface{})

	for _, instanceId := range input.InstanceIds {
		instanceIds[*instanceId] = nil
	}

	for _, instance := range m.Instances {
		if _, ok := instanceIds[instance.InstanceID]; input.InstanceIds != nil && !ok {
			continue
		}

		instances = append(
			instances,
			generateEc2Instance(instance),
		)
	}

	return &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{
			{
				Instances: instances,
			},
		},
	}, nil
}
