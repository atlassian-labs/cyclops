package mock

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

type MockEc2 struct {
	ec2iface.EC2API

	cloudProviderInstances *[]*Node
}

type MockAutoscaling struct {
	autoscalingiface.AutoScalingAPI

	cloudProviderInstances *[]*Node
}

func generateProviderID(node *Node) {
	node.providerID = fmt.Sprintf("aws:///%s/%s",
		defaultAvailabilityZone,
		node.InstanceID,
	)
}

func generateEc2Instance(node *Node) *ec2.Instance {
	instance := &ec2.Instance{
		InstanceId: aws.String(node.InstanceID),
		State: &ec2.InstanceState{
			Name: aws.String(node.state),
		},
	}

	return instance
}

func generateAutoscalingInstance(node *Node) *autoscaling.Instance {
	instance := &autoscaling.Instance{
		InstanceId:       aws.String(node.InstanceID),
		AvailabilityZone: aws.String(defaultAvailabilityZone),
	}

	return instance
}

// *************** Autoscaling *************** //

func (m *MockAutoscaling) DescribeAutoScalingGroups(input *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	var asgs = make(map[string]*autoscaling.Group, 0)

	var asgNameLookup = make(map[string]interface{})

	for _, asgName := range input.AutoScalingGroupNames {
		asgNameLookup[*asgName] = nil
	}

	for _, node := range *m.cloudProviderInstances {
		if node.Nodegroup == "" {
			continue
		}

		if _, exists := asgNameLookup[node.Nodegroup]; !exists {
			continue
		}

		asg, exists := asgs[node.Nodegroup]

		if !exists {
			asg = &autoscaling.Group{
				AutoScalingGroupName: aws.String(node.Nodegroup),
				Instances:            []*autoscaling.Instance{},
				AvailabilityZones: []*string{
					aws.String(defaultAvailabilityZone),
				},
			}

			asgs[node.Nodegroup] = asg
		}

		asg.Instances = append(asg.Instances, generateAutoscalingInstance(node))
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

func (m *MockEc2) DescribeInstances(input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	var instances = make([]*ec2.Instance, 0)
	var instanceIds = make(map[string]interface{})

	for _, instanceId := range input.InstanceIds {
		instanceIds[*instanceId] = nil
	}

	for _, node := range *m.cloudProviderInstances {
		if _, ok := instanceIds[node.InstanceID]; input.InstanceIds != nil && !ok {
			continue
		}

		instances = append(instances, generateEc2Instance(node))
	}

	return &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{
			{
				Instances: instances,
			},
		},
	}, nil
}
