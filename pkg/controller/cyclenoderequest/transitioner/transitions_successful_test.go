package transitioner

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	fakerawclient "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

// Test that cleanup removes annotations from all nodes tracked in AnnotatedNodes
// during the transition to Successful, and that AnnotatedNodes is persisted as empty
// so subsequent requeues are no-ops.
func TestSuccessfulCleansUpAnnotatedNodes(t *testing.T) {
	// Simulate nodes that were annotated across multiple batches.
	// These are replacement nodes that already exist in the cluster.
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

	// Start the CNR in Initialised phase with no NodesToTerminate remaining.
	// checkIfTransitioning will see 0 nodes left to cycle and call
	// transitionToSuccessful, which runs cleanup and persists AnnotatedNodes = nil.
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-cleanup",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
			Selector:      metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
			CycleSettings: v1.CycleSettings{
				Concurrency: 1,
				Method:      v1.CycleNodeRequestMethodDrain,
			},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:            v1.CycleNodeRequestInitialised,
			ActiveChildren:   0,
			NumNodesCycled:   3,
			NodesToTerminate: []v1.CycleNodeRequestNode{},
			AnnotatedNodes:   []string{"node-1", "node-2", "node-3"},
			CurrentNodes:     []v1.CycleNodeRequestNode{},
			ScaleUpStarted:   &metav1.Time{Time: time.Now().Add(-10 * time.Minute)},
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes([]*mock.Node{node1, node2, node3}),
		WithCloudProviderInstances([]*mock.Node{node1, node2, node3}),
		WithExtraKubeObject(nodeGroup),
	)

	// Run from Initialised — should transition to Successful via checkIfTransitioning
	_, err := fakeTransitioner.Run()
	require.NoError(t, err)

	// Verify the CNR transitioned to Successful
	assert.Equal(t, v1.CycleNodeRequestSuccessful, fakeTransitioner.cycleNodeRequest.Status.Phase,
		"CNR should have transitioned to Successful")

	// Verify annotations were removed from all 3 nodes
	for _, name := range []string{"node-1", "node-2", "node-3"} {
		node, err := fakeTransitioner.Client.RawClient.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
		require.NoError(t, err)
		_, hasAnnotation := node.Annotations[clusterAutoscalerScaleDownDisabledAnnotation]
		assert.False(t, hasAnnotation, "scale-down-disabled annotation should be removed from %s", name)
	}

	// Verify AnnotatedNodes was persisted as empty to the API server
	persistedCNR := &v1.CycleNodeRequest{}
	err = fakeTransitioner.K8sClient.Get(context.TODO(),
		types.NamespacedName{Name: "cnr-cleanup", Namespace: "kube-system"}, persistedCNR)
	require.NoError(t, err, "should be able to read CNR back from API server")
	assert.Empty(t, persistedCNR.Status.AnnotatedNodes,
		"AnnotatedNodes should be empty in the persisted CNR (prevents re-running cleanup on requeue)")
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
			CreationTimestamp: metav1.NewTime(time.Now().Add(-6 * 24 * time.Hour)),
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

// Test that annotations are cleaned up per-batch when transitioning from
// WaitingTermination back to Initialised, rather than accumulating until
// the final Successful transition.
func TestWaitingTerminationCleansUpAnnotationsPerBatch(t *testing.T) {
	// A replacement node that was annotated during this batch's ScalingUp.
	replacementNode := &mock.Node{
		Name:            "replacement-1",
		InstanceID:      "i-replacement1",
		Nodegroup:       "ng-1",
		NodeReady:       corev1.ConditionTrue,
		LabelKey:        "role",
		LabelValue:      "worker",
		AnnotationKey:   clusterAutoscalerScaleDownDisabledAnnotation,
		AnnotationValue: clusterAutoscalerScaleDownDisabledValue,
	}

	// A node still waiting to be cycled in a future batch.
	pendingNode := &mock.Node{
		Name:       "pending-1",
		InstanceID: "i-pending1",
		Nodegroup:  "ng-1",
		NodeReady:  corev1.ConditionTrue,
		LabelKey:   "role",
		LabelValue: "worker",
	}

	nodeGroup := &v1.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "ng-1"},
		Spec: v1.NodeGroupSpec{
			NodeGroupName: "ng-1",
			NodeSelector:  metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
		},
	}

	// A completed CNS for this batch (Successful = old node terminated).
	cns := &v1.CycleNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cns-batch1",
			Namespace: "kube-system",
			Labels:    map[string]string{"name": "cnr-perbatch"},
		},
		Status: v1.CycleNodeStatusStatus{
			Phase: v1.CycleNodeStatusSuccessful,
		},
	}

	// CNR is in WaitingTermination with 1 node still available to cycle.
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-perbatch",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
			Selector:      metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
			CycleSettings: v1.CycleSettings{
				Concurrency: 1,
				Method:      v1.CycleNodeRequestMethodDrain,
			},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:          v1.CycleNodeRequestWaitingTermination,
			ActiveChildren: 1,
			NumNodesCycled: 1,
			NodesAvailable: []v1.CycleNodeRequestNode{
				{Name: "pending-1", ProviderID: "i-pending1"},
			},
			NodesToTerminate: []v1.CycleNodeRequestNode{},
			AnnotatedNodes:   []string{"replacement-1"},
			CurrentNodes:     []v1.CycleNodeRequestNode{},
			ScaleUpStarted:   &metav1.Time{Time: time.Now().Add(-5 * time.Minute)},
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes([]*mock.Node{replacementNode, pendingNode}),
		WithCloudProviderInstances([]*mock.Node{replacementNode, pendingNode}),
		WithExtraKubeObject(nodeGroup),
		WithExtraKubeObject(cns),
	)

	// Run from WaitingTermination — should reap the Successful CNS and
	// transition back to Initialised, cleaning up annotations in between.
	_, err := fakeTransitioner.Run()
	require.NoError(t, err)

	// Verify it transitioned back to Initialised
	assert.Equal(t, v1.CycleNodeRequestInitialised, fakeTransitioner.cycleNodeRequest.Status.Phase,
		"CNR should have transitioned back to Initialised for next batch")

	// Verify the annotation was removed from the replacement node
	node, err := fakeTransitioner.Client.RawClient.CoreV1().Nodes().Get(context.TODO(), "replacement-1", metav1.GetOptions{})
	require.NoError(t, err)
	_, hasAnnotation := node.Annotations[clusterAutoscalerScaleDownDisabledAnnotation]
	assert.False(t, hasAnnotation, "scale-down-disabled annotation should be removed after batch completes")

	// Verify AnnotatedNodes was cleared (persisted via UpdateObject in transitionWaitingTermination)
	persistedCNR := &v1.CycleNodeRequest{}
	err = fakeTransitioner.K8sClient.Get(context.TODO(),
		types.NamespacedName{Name: "cnr-perbatch", Namespace: "kube-system"}, persistedCNR)
	require.NoError(t, err)
	assert.Empty(t, persistedCNR.Status.AnnotatedNodes,
		"AnnotatedNodes should be empty after per-batch cleanup")
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

// Test that when some annotation removals fail during transitionToSuccessful,
// AnnotatedNodes retains only the failed nodes so the node controller can
// clean them up (rather than being unconditionally nil'd out).
func TestSuccessfulPartialCleanupFailurePreservesFailedNodes(t *testing.T) {
	nodeOK := &mock.Node{
		Name:            "node-ok",
		InstanceID:      "i-nodeok",
		Nodegroup:       "ng-1",
		NodeReady:       corev1.ConditionTrue,
		LabelKey:        "role",
		LabelValue:      "worker",
		AnnotationKey:   clusterAutoscalerScaleDownDisabledAnnotation,
		AnnotationValue: clusterAutoscalerScaleDownDisabledValue,
	}
	nodeFail := &mock.Node{
		Name:            "node-fail",
		InstanceID:      "i-nodefail",
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
			Name:              "cnr-partial-fail",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
			Selector:      metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
			CycleSettings: v1.CycleSettings{
				Concurrency: 1,
				Method:      v1.CycleNodeRequestMethodDrain,
			},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:            v1.CycleNodeRequestInitialised,
			ActiveChildren:   0,
			NumNodesCycled:   2,
			NodesToTerminate: []v1.CycleNodeRequestNode{},
			AnnotatedNodes:   []string{"node-ok", "node-fail"},
			CurrentNodes:     []v1.CycleNodeRequestNode{},
			ScaleUpStarted:   &metav1.Time{Time: time.Now().Add(-10 * time.Minute)},
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes([]*mock.Node{nodeOK, nodeFail}),
		WithCloudProviderInstances([]*mock.Node{nodeOK, nodeFail}),
		WithExtraKubeObject(nodeGroup),
	)

	// Inject a reactor that makes patch calls fail for "node-fail".
	rawClient := fakeTransitioner.RawClient.(*fakerawclient.Clientset)
	rawClient.PrependReactor("patch", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchAction := action.(k8stesting.PatchAction)
		if patchAction.GetName() == "node-fail" {
			return true, nil, fmt.Errorf("simulated API error")
		}
		return false, nil, nil
	})

	_, err := fakeTransitioner.Run()
	require.NoError(t, err)

	assert.Equal(t, v1.CycleNodeRequestSuccessful, fakeTransitioner.cycleNodeRequest.Status.Phase,
		"CNR should still transition to Successful despite partial failure")

	// node-ok should have its annotation removed successfully.
	nodeResult, err := rawClient.CoreV1().Nodes().Get(context.TODO(), "node-ok", metav1.GetOptions{})
	require.NoError(t, err)
	_, hasAnnotation := nodeResult.Annotations[clusterAutoscalerScaleDownDisabledAnnotation]
	assert.False(t, hasAnnotation, "annotation should be removed from node-ok")

	// AnnotatedNodes should contain only the failed node, NOT be nil.
	persistedCNR := &v1.CycleNodeRequest{}
	err = fakeTransitioner.K8sClient.Get(context.TODO(),
		types.NamespacedName{Name: "cnr-partial-fail", Namespace: "kube-system"}, persistedCNR)
	require.NoError(t, err)
	assert.Equal(t, []string{"node-fail"}, persistedCNR.Status.AnnotatedNodes,
		"AnnotatedNodes should retain only the node that failed cleanup")
}

// Test that transitionToUnsuccessful (Healing/Failed path) nils out AnnotatedNodes
// without attempting removal — the node controller is the backstop for this case.
func TestTransitionToUnsuccessfulNilsAnnotatedNodesWithoutCleanup(t *testing.T) {
	annotatedNode := &mock.Node{
		Name:            "node-annotated",
		InstanceID:      "i-annotated",
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
			Name:              "cnr-healing",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
			Selector:      metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
			CycleSettings: v1.CycleSettings{
				Concurrency: 1,
				Method:      v1.CycleNodeRequestMethodDrain,
			},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:          v1.CycleNodeRequestScalingUp,
			AnnotatedNodes: []string{"node-annotated"},
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes([]*mock.Node{annotatedNode}),
		WithCloudProviderInstances([]*mock.Node{annotatedNode}),
		WithExtraKubeObject(nodeGroup),
	)

	// Directly call transitionToHealing to simulate the failure path.
	_, _ = fakeTransitioner.transitionToHealing(fmt.Errorf("something went wrong"))

	// Verify AnnotatedNodes was nil'd out.
	persistedCNR := &v1.CycleNodeRequest{}
	err := fakeTransitioner.K8sClient.Get(context.TODO(),
		types.NamespacedName{Name: "cnr-healing", Namespace: "kube-system"}, persistedCNR)
	require.NoError(t, err)
	assert.Nil(t, persistedCNR.Status.AnnotatedNodes,
		"AnnotatedNodes should be nil on failure path (no cleanup attempted)")

	// Verify the annotation is still on the node — it was NOT removed.
	rawClient := fakeTransitioner.RawClient.(*fakerawclient.Clientset)
	node, err := rawClient.CoreV1().Nodes().Get(context.TODO(), "node-annotated", metav1.GetOptions{})
	require.NoError(t, err)
	_, hasAnnotation := node.Annotations[clusterAutoscalerScaleDownDisabledAnnotation]
	assert.True(t, hasAnnotation,
		"annotation should remain on node (failure path skips cleanup, node controller will handle it)")
}

// Test that the WaitingTermination → Initialised path preserves failed nodes
// in AnnotatedNodes so they get retried on the next pass.
func TestWaitingTerminationPartialCleanupFailurePreservesForRetry(t *testing.T) {
	nodeOK := &mock.Node{
		Name:            "replacement-ok",
		InstanceID:      "i-repok",
		Nodegroup:       "ng-1",
		NodeReady:       corev1.ConditionTrue,
		LabelKey:        "role",
		LabelValue:      "worker",
		AnnotationKey:   clusterAutoscalerScaleDownDisabledAnnotation,
		AnnotationValue: clusterAutoscalerScaleDownDisabledValue,
	}
	nodeFail := &mock.Node{
		Name:            "replacement-fail",
		InstanceID:      "i-repfail",
		Nodegroup:       "ng-1",
		NodeReady:       corev1.ConditionTrue,
		LabelKey:        "role",
		LabelValue:      "worker",
		AnnotationKey:   clusterAutoscalerScaleDownDisabledAnnotation,
		AnnotationValue: clusterAutoscalerScaleDownDisabledValue,
	}
	pendingNode := &mock.Node{
		Name:       "pending-1",
		InstanceID: "i-pending1",
		Nodegroup:  "ng-1",
		NodeReady:  corev1.ConditionTrue,
		LabelKey:   "role",
		LabelValue: "worker",
	}

	nodeGroup := &v1.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "ng-1"},
		Spec: v1.NodeGroupSpec{
			NodeGroupName: "ng-1",
			NodeSelector:  metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
		},
	}

	// A completed CNS so reapChildren transitions back to Initialised.
	cns := &v1.CycleNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cns-batch",
			Namespace: "kube-system",
			Labels:    map[string]string{"name": "cnr-retry"},
		},
		Status: v1.CycleNodeStatusStatus{
			Phase: v1.CycleNodeStatusSuccessful,
		},
	}

	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-retry",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
			Selector:      metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}},
			CycleSettings: v1.CycleSettings{
				Concurrency: 1,
				Method:      v1.CycleNodeRequestMethodDrain,
			},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:          v1.CycleNodeRequestWaitingTermination,
			ActiveChildren: 1,
			NumNodesCycled: 2,
			NodesAvailable: []v1.CycleNodeRequestNode{
				{Name: "pending-1", ProviderID: "i-pending1"},
			},
			NodesToTerminate: []v1.CycleNodeRequestNode{},
			AnnotatedNodes:   []string{"replacement-ok", "replacement-fail"},
			CurrentNodes:     []v1.CycleNodeRequestNode{},
			ScaleUpStarted:   &metav1.Time{Time: time.Now().Add(-5 * time.Minute)},
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes([]*mock.Node{nodeOK, nodeFail, pendingNode}),
		WithCloudProviderInstances([]*mock.Node{nodeOK, nodeFail, pendingNode}),
		WithExtraKubeObject(nodeGroup),
		WithExtraKubeObject(cns),
	)

	// Inject a reactor that makes patch calls fail for "replacement-fail".
	rawClient := fakeTransitioner.RawClient.(*fakerawclient.Clientset)
	rawClient.PrependReactor("patch", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patchAction := action.(k8stesting.PatchAction)
		if patchAction.GetName() == "replacement-fail" {
			return true, nil, fmt.Errorf("simulated transient API error")
		}
		return false, nil, nil
	})

	_, err := fakeTransitioner.Run()
	require.NoError(t, err)

	assert.Equal(t, v1.CycleNodeRequestInitialised, fakeTransitioner.cycleNodeRequest.Status.Phase,
		"CNR should have transitioned back to Initialised")

	// replacement-ok should have its annotation removed.
	nodeResult, err := rawClient.CoreV1().Nodes().Get(context.TODO(), "replacement-ok", metav1.GetOptions{})
	require.NoError(t, err)
	_, hasAnnotation := nodeResult.Annotations[clusterAutoscalerScaleDownDisabledAnnotation]
	assert.False(t, hasAnnotation, "annotation should be removed from replacement-ok")

	// AnnotatedNodes should retain only the failed node for retry on next pass.
	persistedCNR := &v1.CycleNodeRequest{}
	err = fakeTransitioner.K8sClient.Get(context.TODO(),
		types.NamespacedName{Name: "cnr-retry", Namespace: "kube-system"}, persistedCNR)
	require.NoError(t, err)
	assert.Equal(t, []string{"replacement-fail"}, persistedCNR.Status.AnnotatedNodes,
		"AnnotatedNodes should retain only the failed node for retry")
}
