package transitioner

import (
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/mock"
	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Basic test to ensure the base functionality of the Pending phase works. A
// predictable set of nodes with matching cloud provider instances attached to
// their nodegroups should get the CNR transitioned to the Initialized phase.
func TestPendingSimpleCase(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

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
			Phase: v1.CycleNodeRequestPending,
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
	)

	result, err := fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.True(t, result.Requeue)

	// It should move to the Initialised phase and set up the status of the CNR
	// in a predictable manner
	assert.Equal(t, v1.CycleNodeRequestInitialised, cnr.Status.Phase)
	assert.Len(t, cnr.Status.NodesToTerminate, 2)
	assert.Equal(t, cnr.Status.ActiveChildren, int64(0))
	assert.Equal(t, cnr.Status.NumNodesCycled, 0)
}

// Test to ensure the Pending phase will accept a CNR with a correct named node.
func TestPendingWithNamedNode(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

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
			NodeNames: []string{
				"ng-1-node-0",
			},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase: v1.CycleNodeRequestPending,
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
	)

	result, err := fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.True(t, result.Requeue)

	// It should move to the Initialised phase and set up the status of the CNR
	// in a predictable manner
	assert.Equal(t, v1.CycleNodeRequestInitialised, cnr.Status.Phase)
	assert.Len(t, cnr.Status.NodesToTerminate, 1)
	assert.Equal(t, cnr.Status.ActiveChildren, int64(0))
	assert.Equal(t, cnr.Status.NumNodesCycled, 0)
}

// Test to ensure the Pending phase will reject a CNR with a named node that
// does not match any of the nodes matching the node selector. It should error
// out immediately.
func TestPendingWrongNamedNode(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

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
			NodeNames: []string{
				"ng-1-node-0",
				"ng-1-node-2",
			},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase: v1.CycleNodeRequestPending,
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
	)

	_, err = fakeTransitioner.Run()
	assert.Error(t, err)
	assert.Equal(t, v1.CycleNodeRequestHealing, cnr.Status.Phase)
}

// Test to ensure that if there's a mismatch between the instances found in the
// cloud provider and kube then the CNR will error out immediately rather than
// proceed. Specifically test missing cloud provider instances.
func TestPendingNoCloudProvierNodes(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

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
			Phase: v1.CycleNodeRequestPending,
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
	)

	_, err = fakeTransitioner.Run()
	assert.Error(t, err)
	assert.Equal(t, v1.CycleNodeRequestHealing, cnr.Status.Phase)
}

// Test to ensure that if there's a mismatch between the instances found in the
// cloud provider and kube then the CNR will error out immediately rather than
// proceed. Specifically test missing kube nodes.
func TestPendingNoKubeNodes(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

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
			Phase: v1.CycleNodeRequestPending,
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithCloudProviderInstances(nodegroup),
	)

	_, err = fakeTransitioner.Run()
	assert.Error(t, err)
	assert.Equal(t, v1.CycleNodeRequestHealing, cnr.Status.Phase)
}

// Test to ensure that Cyclops will not proceed if there is node detached from
// the nodegroup on the cloud provider. It should try to wait for the issue to
// resolve transition to the Healing phase if it doesn't.
func TestPendingDetachedCloudProviderNode(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

	// "detach" one instance from the asg
	nodegroup[0].Nodegroup = ""

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
			Phase: v1.CycleNodeRequestPending,
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
	)

	// Should requeue while it tries to wait
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestPending, cnr.Status.Phase)

	// Simulate waiting for 1s more than the wait limit
	cnr.Status.EquilibriumWaitStarted = &metav1.Time{
		Time: time.Now().Add(-nodeEquilibriumWaitLimit - time.Second),
	}

	// This time should transition to the healing phase
	_, err = fakeTransitioner.Run()
	assert.Error(t, err)
	assert.Equal(t, v1.CycleNodeRequestHealing, cnr.Status.Phase)
}

// Test that if no nodegroup names are given. The CNR should transition to the
// Healing phase since no nodes will match in the cloud provider.
func TestPendingNoNodegroupNamesGiven(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cnr-1",
			Namespace: "kube-system",
		},
		Spec: v1.CycleNodeRequestSpec{
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
			Phase: v1.CycleNodeRequestPending,
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
	)

	_, err = fakeTransitioner.Run()
	assert.Error(t, err)
	assert.Equal(t, v1.CycleNodeRequestHealing, cnr.Status.Phase)
}

// Test that if there is a mismatching nodegroup name, the CNR should transition
// to the Healing phase since there will be no nodes matching.
func TestPendingMismatchingNodegroupName(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cnr-1",
			Namespace: "kube-system",
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupsList: []string{"ng-2"},
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
			Phase: v1.CycleNodeRequestPending,
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
	)

	_, err = fakeTransitioner.Run()
	assert.Error(t, err)
	assert.Equal(t, v1.CycleNodeRequestHealing, cnr.Status.Phase)
}
