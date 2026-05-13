package transitioner

import (
	"context"
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Test that if no delete options are given to the transitioner, then the CNR
// will not be deleted.
func TestSuccessfulNoDelete(t *testing.T) {
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-1",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestSuccessful,
			Message: "",
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr)

	var list v1.CycleNodeRequestList

	assert.NoError(t,
		fakeTransitioner.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)

	// Execute the Successful phase
	_, err := fakeTransitioner.Run()
	assert.NoError(t, err)

	assert.NoError(t,
		fakeTransitioner.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)
}

// Test that cleanup removes annotations from all nodes tracked in AnnotatedNodes,
// and clears the list afterwards so subsequent requeues are no-ops.
func TestSuccessfulCleansUpAnnotatedNodes(t *testing.T) {
	// Simulate nodes that were annotated across multiple batches.
	node1 := &mock.Node{
		Name:            "node-1",
		InstanceID:      "i-node1",
		Nodegroup:       "ng-1",
		NodeReady:       corev1.ConditionTrue,
		LabelKey:        "role",
		LabelValue:      "worker",
		AnnotationKey:   clusterAutoscalerScaleDownDisabledAnnotation,
		AnnotationValue: clusterAutoscalerScaleDownDisabledValue,
	}
	node2 := &mock.Node{
		Name:            "node-2",
		InstanceID:      "i-node2",
		Nodegroup:       "ng-1",
		NodeReady:       corev1.ConditionTrue,
		LabelKey:        "role",
		LabelValue:      "worker",
		AnnotationKey:   clusterAutoscalerScaleDownDisabledAnnotation,
		AnnotationValue: clusterAutoscalerScaleDownDisabledValue,
	}
	node3 := &mock.Node{
		Name:            "node-3",
		InstanceID:      "i-node3",
		Nodegroup:       "ng-1",
		NodeReady:       corev1.ConditionTrue,
		LabelKey:        "role",
		LabelValue:      "worker",
		AnnotationKey:   clusterAutoscalerScaleDownDisabledAnnotation,
		AnnotationValue: clusterAutoscalerScaleDownDisabledValue,
	}

	nodeGroup := &v1.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "ng-1"},
		Spec: v1.NodeGroupSpec{
			NodeGroupName: "ng-1",
			NodeSelector:  metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
		},
	}

	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-cleanup",
			Namespace:         "kube-system",
			CreationTimestamp:  metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
			Selector:      metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
			CycleSettings: v1.CycleSettings{},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:          v1.CycleNodeRequestSuccessful,
			AnnotatedNodes: []string{"node-1", "node-2", "node-3"},
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes([]*mock.Node{node1, node2, node3}),
		WithCloudProviderInstances([]*mock.Node{node1, node2, node3}),
		WithExtraKubeObject(nodeGroup),
	)

	// Run the Successful phase
	_, err := fakeTransitioner.Run()
	require.NoError(t, err)

	// Verify annotations were removed from all 3 nodes
	for _, name := range []string{"node-1", "node-2", "node-3"} {
		node, err := fakeTransitioner.Client.RawClient.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
		require.NoError(t, err)
		_, hasAnnotation := node.Annotations[clusterAutoscalerScaleDownDisabledAnnotation]
		assert.False(t, hasAnnotation, "scale-down-disabled annotation should be removed from %s", name)
	}

	// Verify AnnotatedNodes was cleared
	assert.Empty(t, fakeTransitioner.cycleNodeRequest.Status.AnnotatedNodes,
		"AnnotatedNodes should be empty after successful cleanup")
}

// Test that when AnnotatedNodes is empty, cleanup is a no-op and does not
// interfere with other CNRs. This is the core fix for Bug 2.
func TestSuccessfulCleanupIsNoOpWhenAnnotatedNodesEmpty(t *testing.T) {
	// This node belongs to a different, active CNR. It has the annotation set.
	otherNode := &mock.Node{
		Name:            "other-node",
		InstanceID:      "i-other",
		Nodegroup:       "ng-1",
		NodeReady:       corev1.ConditionTrue,
		LabelKey:        "role",
		LabelValue:      "worker",
		AnnotationKey:   clusterAutoscalerScaleDownDisabledAnnotation,
		AnnotationValue: clusterAutoscalerScaleDownDisabledValue,
		Creation:        time.Now().Add(-1 * time.Hour),
	}

	nodeGroup := &v1.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "ng-1"},
		Spec: v1.NodeGroupSpec{
			NodeGroupName: "ng-1",
			NodeSelector:  metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
		},
	}

	// An OLD Successful CNR that already cleaned up. AnnotatedNodes is empty.
	// With the old time-based code, ScaleUpStarted from 6 days ago would cause
	// this CNR to find and strip annotations from otherNode.
	oldTime := metav1.NewTime(time.Now().Add(-6 * 24 * time.Hour))
	oldCNR := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-old-successful",
			Namespace:         "kube-system",
			CreationTimestamp:  metav1.NewTime(time.Now().Add(-6 * 24 * time.Hour)),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
			Selector:      metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
			CycleSettings: v1.CycleSettings{},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:          v1.CycleNodeRequestSuccessful,
			ScaleUpStarted: &oldTime,
			AnnotatedNodes: nil, // Already cleaned up
		},
	}

	fakeTransitioner := NewFakeTransitioner(oldCNR,
		WithKubeNodes([]*mock.Node{otherNode}),
		WithCloudProviderInstances([]*mock.Node{otherNode}),
		WithExtraKubeObject(nodeGroup),
	)

	// Run the Successful phase (simulating a pod restart re-queue)
	_, err := fakeTransitioner.Run()
	require.NoError(t, err)

	// The critical assertion: otherNode's annotation must still be present.
	// Bug 2 would have removed it because ScaleUpStarted is 6 days old.
	node, err := fakeTransitioner.Client.RawClient.CoreV1().Nodes().Get(context.TODO(), "other-node", metav1.GetOptions{})
	require.NoError(t, err)
	val, hasAnnotation := node.Annotations[clusterAutoscalerScaleDownDisabledAnnotation]
	assert.True(t, hasAnnotation,
		"annotation must NOT be removed from other-node by an old Successful CNR")
	assert.Equal(t, clusterAutoscalerScaleDownDisabledValue, val)
}

// Test that if the phase executes before the CNR deletion expiry time then the
// CNR won't be deleted.
func TestSuccessfulAfterDeleteTime(t *testing.T) {
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-1",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestSuccessful,
			Message: "",
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithTransitionerOptions(Options{
			DeleteCNR:       true,
			DeleteCNRExpiry: 0 * time.Second,
		}),
	)

	var list v1.CycleNodeRequestList

	assert.NoError(t,
		fakeTransitioner.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)

	// Execute the Successful phase
	_, err := fakeTransitioner.Run()
	assert.NoError(t, err)

	assert.NoError(t,
		fakeTransitioner.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 0)
}

// Test that if the phase executes after the CNR deletion expiry time then the
// CNR will be deleted.
func TestSuccessfulBeforeDeleteTime(t *testing.T) {
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-1",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestSuccessful,
			Message: "",
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithTransitionerOptions(Options{
			DeleteCNR:       true,
			DeleteCNRExpiry: 5 * time.Second,
		}),
	)

	var list v1.CycleNodeRequestList

	assert.NoError(t,
		fakeTransitioner.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)

	// Execute the Successful phase
	_, err := fakeTransitioner.Run()
	assert.NoError(t, err)

	assert.NoError(t,
		fakeTransitioner.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)
}

// Test that the Successful phase will delete sibling CNRs for the same
// nodegroup that are in the failed phase created at the same time or before the
// Successful CNR execution. No other CNRs should be deleted.
func TestSuccessfulDeleteFailedSiblingCNRs(t *testing.T) {
	// CNR used for the execution
	// Should NOT be deleted
	cnr1 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-1",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestSuccessful,
			Message: "",
		},
	}

	// Failed CNR for the same nodegroup created at the same time as cnr1
	// Should be deleted
	cnr2 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-2",
			Namespace:         "kube-system",
			CreationTimestamp: cnr1.CreationTimestamp,
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestFailed,
			Message: "",
		},
	}

	// Failed CNR for the same nodegroup created before cnr1
	// Should NOT be deleted
	cnr3 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-3",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.NewTime(cnr1.CreationTimestamp.Add(-time.Second)),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestFailed,
			Message: "",
		},
	}

	// Failed CNR for the same nodegroup created after cnr1
	// Should NOT be deleted
	cnr4 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-4",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.NewTime(cnr1.CreationTimestamp.Add(time.Second)),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestFailed,
			Message: "",
		},
	}

	// Pending CNR for the same nodegroup
	// Should NOT be deleted
	cnr5 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-5",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.NewTime(cnr1.CreationTimestamp.Add(-time.Second)),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestPending,
			Message: "",
		},
	}

	// Failed CNR for a different nodegroup
	// Should NOT be deleted
	cnr6 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-6",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.NewTime(cnr1.CreationTimestamp.Add(-time.Second)),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-2",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestFailed,
			Message: "",
		},
	}

	// Failed CNR for the same nodegroup in a different namespace
	// Should NOT be deleted
	cnr7 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-7",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(cnr1.CreationTimestamp.Add(-time.Second)),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestFailed,
			Message: "",
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr1,
		WithExtraKubeObject(cnr2),
		WithExtraKubeObject(cnr3),
		WithExtraKubeObject(cnr4),
		WithExtraKubeObject(cnr5),
		WithExtraKubeObject(cnr6),
		WithExtraKubeObject(cnr7),
	)

	var list v1.CycleNodeRequestList

	assert.NoError(t,
		fakeTransitioner.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 7)

	// Execute the Successful phase
	_, err := fakeTransitioner.Run()
	assert.NoError(t, err)

	assert.NoError(t,
		fakeTransitioner.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 5)

	var cnr v1.CycleNodeRequest

	assert.NoError(t, fakeTransitioner.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr1.Name,
		Namespace: cnr1.Namespace,
	}, &cnr))

	// CNR 2 should be deleted
	assert.Error(t, fakeTransitioner.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr2.Name,
		Namespace: cnr2.Namespace,
	}, &cnr))

	// CNR 3 should be deleted
	assert.Error(t, fakeTransitioner.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr3.Name,
		Namespace: cnr3.Namespace,
	}, &cnr))

	assert.NoError(t, fakeTransitioner.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr4.Name,
		Namespace: cnr4.Namespace,
	}, &cnr))

	assert.NoError(t, fakeTransitioner.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr5.Name,
		Namespace: cnr5.Namespace,
	}, &cnr))

	assert.NoError(t, fakeTransitioner.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr6.Name,
		Namespace: cnr6.Namespace,
	}, &cnr))

	assert.NoError(t, fakeTransitioner.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr7.Name,
		Namespace: cnr7.Namespace,
	}, &cnr))
}
