package transitioner

import (
	"context"
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/mock"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test to ensure that a node which is dropped from the cycle during the
// ScalingUp phase does not keep the cycling finalizer. The node selected for
// cycling stops being Ready while the CNR waits for the replacement node. No
// CycleNodeStatus is created for the node, so nothing would remove the
// finalizer later.
func TestScalingUpNodePrematurelyTerminated(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

	// The first node is the node selected for cycling. Its instance is gone,
	// so the kubelet stopped reporting. The second node is the replacement.
	nodegroup[0].NodeReady = corev1.ConditionUnknown

	scaleUpStarted := metav1.NewTime(time.Now().Add(-2 * time.Minute))

	// CNR after the Initialised phase added the finalizer and detached the instance
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
			Phase:          v1.CycleNodeRequestScalingUp,
			ScaleUpStarted: &scaleUpStarted,
			NodesToTerminate: []v1.CycleNodeRequestNode{
				{
					Name:          nodegroup[0].Name,
					NodeGroupName: nodegroup[0].Nodegroup,
				},
			},
			CurrentNodes: []v1.CycleNodeRequestNode{
				{
					Name:          nodegroup[0].Name,
					NodeGroupName: nodegroup[0].Nodegroup,
				},
			},
		},
	}

	// Only the replacement instance is in the cloud provider nodegroup
	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup[1:]),
	)

	// Populate the provider id because it gets generated in NewFakeTransitioner
	cnr.Status.NodesToTerminate[0].ProviderID = fakeTransitioner.KubeNodes[0].ProviderID
	cnr.Status.CurrentNodes[0].ProviderID = fakeTransitioner.KubeNodes[0].ProviderID

	assert.NoError(t, fakeTransitioner.rm.AddFinalizerToNode(nodegroup[0].Name))

	// Execute the ScalingUp phase. The node is dropped from the cycle.
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestCordoningNode, cnr.Status.Phase)
	assert.Len(t, cnr.Status.CurrentNodes, 0)

	// Ensure the dropped node does not have the cycling finalizer
	node, err := fakeTransitioner.rm.RawClient.CoreV1().Nodes().Get(
		context.TODO(), nodegroup[0].Name, metav1.GetOptions{},
	)

	assert.NoError(t, err)
	assert.Len(t, node.Finalizers, 0)
}
