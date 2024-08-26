package transitioner

import (
	"context"
	"testing"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/mock"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Base case of the Initialized phase. Start cycling by detaching an instance
// and adding the cycling finalizer and annotation to it.
func TestInitializedSimpleCase(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 1)
	if err != nil {
		assert.NoError(t, err)
	}

	// CNR straight after being transitioned from Pending
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cnr-1",
			Namespace: "kube-system",
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupsList: []string{"ng-1"},
			CycleSettings: v1.CycleSettings{
				Concurrency: 1,
				Method:      v1.CycleNodeRequestMethodDrain,
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"customer": "kitt",
				},
			},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase: v1.CycleNodeRequestInitialised,
			NodesToTerminate: []v1.CycleNodeRequestNode{
				{
					Name:          nodegroup[0].Name,
					NodeGroupName: nodegroup[0].Nodegroup,
				},
			},
			NodesAvailable: []v1.CycleNodeRequestNode{
				{
					Name:          nodegroup[0].Name,
					NodeGroupName: nodegroup[0].Nodegroup,
				},
			},
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
	)

	// Populate the provider id because it gets generated in NewFakeTransitioner
	cnr.Status.NodesToTerminate[0].ProviderID = fakeTransitioner.KubeNodes[0].ProviderID
	cnr.Status.NodesAvailable[0].ProviderID = fakeTransitioner.KubeNodes[0].ProviderID

	// Execute the Initialized phase
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestScalingUp, cnr.Status.Phase)
	assert.Len(t, cnr.Status.NodesToTerminate, 1)
	assert.Len(t, cnr.Status.NodesAvailable, 0)

	// Ensure that the instance is detached from the ASG
	output, err := fakeTransitioner.Autoscaling.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})
	assert.NoError(t, err)

	// Quirk of the mocking, it constructs the list of ASGs from the list of
	// nodes so 0 ASGs here means that the node has been detached
	assert.Equal(t, 0, len(output.AutoScalingGroups))

	// Ensure the node has the cycling finalizer applied to it
	node, err := fakeTransitioner.rm.RawClient.CoreV1().Nodes().Get(
		context.TODO(), nodegroup[0].Name, metav1.GetOptions{},
	)

	assert.NoError(t, err)
	assert.Len(t, node.Finalizers, 1)

	// Ensure the node has the cycling annotation applied to it
	nodegroupName, err := fakeTransitioner.rm.GetNodegroupFromNodeAnnotation(nodegroup[0].Name)
	assert.NoError(t, err)
	assert.Equal(t, "ng-1", nodegroupName)
}
