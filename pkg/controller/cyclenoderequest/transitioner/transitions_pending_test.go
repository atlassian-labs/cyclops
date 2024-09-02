package transitioner

import (
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/mock"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
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

// Test to ensure the Pending phase will reject a CNR with a named node that
// does not match any of the nodes matching the node selector if strict
// validation is enabled. It should error out immediately.
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

// Test to ensure the Pending phase will accept a CNR with a named node that
// does not match any of the nodes matching the node selector if strict
// validation is not enabled. It will just select the select the nodes that
// exist.
func TestPendingWrongNamedNodeSkipMissingNamedNodes(t *testing.T) {
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
			ValidationOptions: v1.ValidationOptions{
				SkipMissingNamedNodes: true,
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
// does not match any of the nodes matching the node selector if strict
// validation is enabled. It should error out immediately.
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

// Test to ensure that node objects that exist in the cluster without a matching
// instance in the cloud provider are cleaned up and then cycling can proceed
// as normal.
func TestPendingOrphanedKubeNodes(t *testing.T) {
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
		WithCloudProviderInstances(nodegroup[:1]),
	)

	// The first run of the transitioner should go through and try to fix the
	// inconsistency between kube and the cloud provider and will requeue the
	// Pending phase to check again.
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestPending, cnr.Status.Phase)

	// The existing instance in the cloud provider should still be the only one
	output, err := fakeTransitioner.Ec2.DescribeInstances(&ec2.DescribeInstancesInput{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(output.Reservations[0].Instances))

	// After running again, the orphaned kube node is observed to have been
	// removed and the CNR was transitioned to the Initialized phase
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestInitialised, cnr.Status.Phase)
	assert.Len(t, cnr.Status.NodesToTerminate, 1)
}

// Test to ensure that an instance detached from one of the CNR named cloud
// provider nodegroups is re-attached before proceeding with cycling.
func TestPendingDetachedCloudProviderInstance(t *testing.T) {
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

	// Ensure there's only one instance in the ASG
	output, err := fakeTransitioner.Autoscaling.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(output.AutoScalingGroups))
	assert.Equal(t, 1, len(output.AutoScalingGroups[0].Instances))

	// The first run of the transitioner should go through and try to fix the
	// inconsistency between kube and the cloud provider and will requeue the
	// Pending phase to check again.
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestPending, cnr.Status.Phase)

	// Ensure both instance are now in the ASG
	output, err = fakeTransitioner.Autoscaling.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(output.AutoScalingGroups))
	assert.Equal(t, 2, len(output.AutoScalingGroups[0].Instances))

	// This time should transition to the initialized phase
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestInitialised, cnr.Status.Phase)
	assert.Len(t, cnr.Status.NodesToTerminate, 2)
}

// Test to ensure that a detached instance with a matching kube object that does
// not have the cycling annotation to identify it's original cloud provider
// nodegroup should cause the cycling to fail immediately since this is a case
// that cannot be fixed automatically.
func TestPendingDetachedCloudProviderInstanceNoAnnotation(t *testing.T) {
	nodegroup, err := mock.NewNodegroup("ng-1", 2)
	if err != nil {
		assert.NoError(t, err)
	}

	// "detach" one instance from the asg and simulate the node not having
	// the annotation
	nodegroup[0].Nodegroup = ""
	nodegroup[0].AnnotationKey = ""
	nodegroup[0].AnnotationValue = ""

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

	// Ensure there's only one instance in the ASG
	output, err := fakeTransitioner.Autoscaling.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(output.AutoScalingGroups))
	assert.Equal(t, 1, len(output.AutoScalingGroups[0].Instances))

	// The first run of the transitioner should go through and try to fix the
	// inconsistency between kube and the cloud provider. However, the node
	// object for the detached instance does not have the nodegroup annotation
	// so it cannot be re-attached. The CNR should fail because this cannot be
	// fixed.
	_, err = fakeTransitioner.Run()
	assert.Error(t, err)
	assert.Equal(t, v1.CycleNodeRequestHealing, cnr.Status.Phase)
}

// Test to ensure that if the instance state cannot be fixed during the
// equilibrium timeout period, then cycling is failed.
func TestPendingTimeoutReached(t *testing.T) {
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

	output, err := fakeTransitioner.Autoscaling.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(output.AutoScalingGroups))
	assert.Equal(t, 1, len(output.AutoScalingGroups[0].Instances))

	// The first run of the transitioner should go through and try to fix the
	// inconsistency between kube and the cloud provider and will requeue the
	// Pending phase to check again.
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestPending, cnr.Status.Phase)

	// Keep the node detached
	_, err = fakeTransitioner.Autoscaling.DetachInstances(&autoscaling.DetachInstancesInput{
		InstanceIds: aws.StringSlice([]string{nodegroup[0].InstanceID}),
	})

	assert.NoError(t, err)

	output, err = fakeTransitioner.Autoscaling.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(output.AutoScalingGroups))
	assert.Equal(t, 1, len(output.AutoScalingGroups[0].Instances))

	// Simulate waiting for 1s more than the wait limit
	cnr.Status.EquilibriumWaitStarted = &metav1.Time{
		Time: time.Now().Add(-nodeEquilibriumWaitLimit - time.Second),
	}

	// This time should transition to the healing phase
	_, err = fakeTransitioner.Run()
	assert.Error(t, err)
	assert.Equal(t, v1.CycleNodeRequestHealing, cnr.Status.Phase)
}

// Test to ensure that Cyclops will not proceed if there is node detached from
// the nodegroup on the cloud provider. It should try to wait for the issue to
// resolve and transition to Initialised when it does before reaching the
// timeout period.
func TestPendingReattachedCloudProviderNode(t *testing.T) {
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

	// Simulate waiting for 1s less than the wait limit
	cnr.Status.EquilibriumWaitStarted = &metav1.Time{
		Time: time.Now().Add(-nodeEquilibriumWaitLimit + time.Second),
	}

	_, err = fakeTransitioner.Autoscaling.AttachInstances(&autoscaling.AttachInstancesInput{
		AutoScalingGroupName: aws.String("ng-1"),
		InstanceIds:          aws.StringSlice([]string{nodegroup[0].InstanceID}),
	})

	assert.NoError(t, err)

	// "re-attach" the instance to the asg
	fakeTransitioner.CloudProviderInstances[0].Nodegroup = "ng-1"

	// The CNR should transition to the Initialised phase because the state of
	// the nodes is now correct and this happened within the timeout period.
	_, err = fakeTransitioner.Run()
	assert.NoError(t, err)
	assert.Equal(t, v1.CycleNodeRequestInitialised, cnr.Status.Phase)
	assert.Len(t, cnr.Status.NodesToTerminate, 2)
}

// Test to ensure that Cyclops will not proceed if there is node detached from
// the nodegroup on the cloud provider. It should wait and especially should not
// succeed if the instance is re-attached by the final requeuing of the Pending
// phase which would occur after the timeout period.
func TestPendingReattachedCloudProviderNodeTooLate(t *testing.T) {
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

	_, err = fakeTransitioner.Autoscaling.AttachInstances(&autoscaling.AttachInstancesInput{
		AutoScalingGroupName: aws.String("ng-1"),
		InstanceIds:          aws.StringSlice([]string{nodegroup[0].InstanceID}),
	})

	assert.NoError(t, err)

	// "re-attach" the instance to the asg
	fakeTransitioner.CloudProviderInstances[0].Nodegroup = "ng-1"

	// This time should transition to the healing phase even though the state
	// is correct because the timeout check happens first
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
