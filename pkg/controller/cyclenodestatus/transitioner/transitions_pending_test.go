package transitioner

import (
	"context"
	"testing"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/atlassian-labs/cyclops/pkg/mock"
	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test to ensure that a node whose instance is terminated does not keep the
// cycling finalizer. The CNS finishes without reaching the Deleting phase,
// which is the phase that normally removes the finalizer.
func TestPendingInstanceTerminated(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 1)
	if err != nil {
		assert.NoError(t, err)
	}

	cns := &v1.CycleNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cns-1",
			Namespace: "kube-system",
		},
		Spec: v1.CycleNodeStatusSpec{
			NodeName: nodegroup[0].Name,
			CycleSettings: v1.CycleSettings{
				Concurrency: 1,
				Method:      v1.CycleNodeRequestMethodDrain,
			},
		},
		Status: v1.CycleNodeStatusStatus{
			Phase: v1.CycleNodeStatusPending,
		},
	}

	// The node exists in kube but its instance is terminated
	nodegroup[0].CloudProviderState = "terminated"
	client := mock.NewClient(nodegroup, nodegroup, cns)

	rm := &controller.ResourceManager{
		Client:        client.K8sClient,
		RawClient:     client.RawClient,
		CloudProvider: client.CloudProvider,
	}

	// The CNR added the finalizer before it created the CNS
	assert.NoError(t, rm.AddFinalizerToNode(nodegroup[0].Name))

	// Execute the Pending phase
	_, err = NewCycleNodeStatusTransitioner(cns, rm, Options{}).Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeStatusSuccessful, cns.Status.Phase)

	// Ensure the node does not have the cycling finalizer
	node, err := rm.RawClient.CoreV1().Nodes().Get(
		context.TODO(), nodegroup[0].Name, metav1.GetOptions{},
	)

	assert.NoError(t, err)
	assert.Len(t, node.Finalizers, 0)
}
