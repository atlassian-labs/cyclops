package transitioner

import (
	"context"
	"errors"
	"testing"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	fakeaws "github.com/atlassian-labs/cyclops/pkg/cloudprovider/aws/fake"
	"github.com/atlassian-labs/cyclops/pkg/mock"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakerawclient "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

// Test to ensure that a node which is dropped from the cycle does not keep the
// cycling finalizer. The instance leaves InService between the describe call
// and the detach call, for example because Cluster Autoscaler terminated it.
// No CycleNodeStatus is created for the node, so nothing would remove the
// finalizer later and the node object would outlive its instance.
func TestInitializedInstanceAlreadyDetaching(t *testing.T) {
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

	// The ASG rejects the detach because the instance is already terminating
	fakeTransitioner.Autoscaling.(*fakeaws.Autoscaling).DetachInstancesErr = awserr.New(
		"ValidationError",
		"The instance "+nodegroup[0].InstanceID+" is not in InService or Standby.",
		nil,
	)

	// Execute the Initialized phase. The node is skipped, not failed.
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestScalingUp, cnr.Status.Phase)
	assert.Len(t, cnr.Status.CurrentNodes, 0)

	// Ensure the skipped node does not have the cycling finalizer
	node, err := fakeTransitioner.rm.RawClient.CoreV1().Nodes().Get(
		context.TODO(), nodegroup[0].Name, metav1.GetOptions{},
	)

	assert.NoError(t, err)
	assert.Len(t, node.Finalizers, 0)
}

// Test to ensure that the instance is re-attached when the finalizer cannot be
// added after the detach. The finalizer is added after the detach, so a failure
// here leaves a detached instance for the Healing phase to fix.
func TestInitializedAddFinalizerFailsAfterDetach(t *testing.T) {
	// Two nodes, because the fake drops an ASG which has no attached instance
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
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
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
	)

	for _, node := range fakeTransitioner.KubeNodes {
		cnrNode := v1.CycleNodeRequestNode{
			Name:          node.Name,
			NodeGroupName: node.Nodegroup,
			ProviderID:    node.ProviderID,
		}

		cnr.Status.NodesToTerminate = append(cnr.Status.NodesToTerminate, cnrNode)
		cnr.Status.NodesAvailable = append(cnr.Status.NodesAvailable, cnrNode)
	}

	// Fail the first node update, which is the update that adds the finalizer
	failed := false

	fakeTransitioner.RawClient.(*fakerawclient.Clientset).PrependReactor("update", "nodes",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			if failed {
				return false, nil, nil
			}

			failed = true
			return true, nil, errors.New("node update failed")
		},
	)

	// Execute the Initialized phase
	_, err = fakeTransitioner.Run()
	assert.Error(t, err)
	assert.Equal(t, v1.CycleNodeRequestHealing, cnr.Status.Phase)

	// Ensure that one instance is detached from the ASG at this point
	output, err := fakeTransitioner.Autoscaling.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})
	assert.NoError(t, err)
	assert.Len(t, output.AutoScalingGroups, 1)
	assert.Len(t, output.AutoScalingGroups[0].Instances, 1)

	// Execute the Healing phase
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestFailed, cnr.Status.Phase)

	// Ensure both instances are attached to the ASG again
	output, err = fakeTransitioner.Autoscaling.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})
	assert.NoError(t, err)
	assert.Len(t, output.AutoScalingGroups, 1)
	assert.Len(t, output.AutoScalingGroups[0].Instances, 2)

	// Ensure no node has the cycling finalizer
	for _, kubeNode := range nodegroup {
		node, err := fakeTransitioner.rm.RawClient.CoreV1().Nodes().Get(
			context.TODO(), kubeNode.Name, metav1.GetOptions{},
		)

		assert.NoError(t, err)
		assert.Len(t, node.Finalizers, 0)
	}
}
