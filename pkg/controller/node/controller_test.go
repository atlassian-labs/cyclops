package node

import (
	"context"
	"testing"
	"time"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/atlassian-labs/cyclops/pkg/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fakerawclient "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const testNamespace = "kube-system"

func testScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = atlassianv1.SchemeBuilder.AddToScheme(scheme)
	return scheme
}

func newTestReconciler(objs ...client.Object) (*Reconciler, *fakerawclient.Clientset) {
	scheme := testScheme()

	// Extract corev1.Node objects for the rawClient (which operates on runtime.Object).
	var runtimeNodes []runtime.Object
	for _, obj := range objs {
		if n, ok := obj.(*corev1.Node); ok {
			runtimeNodes = append(runtimeNodes, n.DeepCopy())
		}
	}

	fakeClient := fakeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
	rawClient := fakerawclient.NewSimpleClientset(runtimeNodes...)

	r := &Reconciler{
		client:    fakeClient,
		rawClient: rawClient,
		namespace: testNamespace,
		options:   Options{}.withDefaults(),
	}
	return r, rawClient
}

func testNode(name string, nodeLabels, annotations map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      nodeLabels,
			Annotations: annotations,
		},
	}
}

func testNodeGroup(name string, matchLabels map[string]string) *atlassianv1.NodeGroup {
	return &atlassianv1.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: atlassianv1.NodeGroupSpec{
			NodeGroupName: name,
			NodeSelector: metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			CycleSettings: atlassianv1.CycleSettings{
				Method:      "Drain",
				Concurrency: 1,
			},
		},
	}
}

func testCNR(name string, phase atlassianv1.CycleNodeRequestPhase, matchLabels map[string]string) *atlassianv1.CycleNodeRequest {
	return &atlassianv1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: atlassianv1.CycleNodeRequestSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
			CycleSettings: atlassianv1.CycleSettings{
				Method:      "Drain",
				Concurrency: 1,
			},
		},
		Status: atlassianv1.CycleNodeRequestStatus{
			Phase: phase,
		},
	}
}

func bothAnnotations() map[string]string {
	return map[string]string{
		k8s.CyclopsManagedAnnotation:                     "true",
		k8s.ClusterAutoscalerScaleDownDisabledAnnotation: "true",
	}
}

var workerLabels = map[string]string{"node-role": "worker"}

func requestFor(name string) reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}
}

// getNodeAnnotations fetches the node from the rawClient and returns its annotations.
func getNodeAnnotations(t *testing.T, rawClient *fakerawclient.Clientset, name string) map[string]string {
	t.Helper()
	node, err := rawClient.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
	require.NoError(t, err)
	return node.Annotations
}

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestReconcile(t *testing.T) {
	tests := []struct {
		name            string
		objects         []client.Object
		nodeName        string
		expectRequeue   bool
		expectRemoved   bool
		expectUnchanged bool
	}{
		{
			name: "no annotations, with nodegroup",
			objects: []client.Object{
				testNode("node-1", workerLabels, nil),
				testNodeGroup("ng-1", workerLabels),
			},
			nodeName:        "node-1",
			expectUnchanged: true,
		},
		{
			name: "no annotations, no nodegroup",
			objects: []client.Object{
				testNode("node-1", workerLabels, nil),
			},
			nodeName:        "node-1",
			expectUnchanged: true,
		},
		{
			name: "both annotations, no nodegroup",
			objects: []client.Object{
				testNode("node-1", workerLabels, bothAnnotations()),
			},
			nodeName:        "node-1",
			expectUnchanged: true,
		},
		{
			name: "both annotations, with nodegroup, no active CNR",
			objects: []client.Object{
				testNode("node-1", workerLabels, bothAnnotations()),
				testNodeGroup("ng-1", workerLabels),
			},
			nodeName:      "node-1",
			expectRemoved: true,
		},
		{
			name: "both annotations, with nodegroup, active CNR matching",
			objects: []client.Object{
				testNode("node-1", workerLabels, bothAnnotations()),
				testNodeGroup("ng-1", workerLabels),
				testCNR("cnr-1", atlassianv1.CycleNodeRequestScalingUp, workerLabels),
			},
			nodeName:      "node-1",
			expectRequeue: true,
		},
		{
			name: "both annotations, with nodegroup, only terminal CNRs",
			objects: []client.Object{
				testNode("node-1", workerLabels, bothAnnotations()),
				testNodeGroup("ng-1", workerLabels),
				testCNR("cnr-success", atlassianv1.CycleNodeRequestSuccessful, workerLabels),
				testCNR("cnr-failed", atlassianv1.CycleNodeRequestFailed, workerLabels),
			},
			nodeName:      "node-1",
			expectRemoved: true,
		},
		{
			name: "only cyclopsManagedAnnotation present (partial cleanup recovery)",
			objects: []client.Object{
				testNode("node-1", workerLabels, map[string]string{
					cyclopsManagedAnnotation: "true",
				}),
				testNodeGroup("ng-1", workerLabels),
			},
			nodeName:      "node-1",
			expectRemoved: true,
		},
		{
			name: "only clusterAutoscalerScaleDownDisabledAnnotation present",
			objects: []client.Object{
				testNode("node-1", workerLabels, map[string]string{
					clusterAutoscalerScaleDownDisabledAnnotation: "true",
				}),
				testNodeGroup("ng-1", workerLabels),
			},
			nodeName:        "node-1",
			expectUnchanged: true,
		},
		{
			name:            "node not found",
			objects:         []client.Object{},
			nodeName:        "nonexistent",
			expectUnchanged: true,
		},
		{
			name: "active CNR with non-matching selector",
			objects: []client.Object{
				testNode("node-1", workerLabels, bothAnnotations()),
				testNodeGroup("ng-1", workerLabels),
				testCNR("cnr-other", atlassianv1.CycleNodeRequestScalingUp, map[string]string{"node-role": "infra"}),
			},
			nodeName:      "node-1",
			expectRemoved: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, rawClient := newTestReconciler(tc.objects...)
			ctx := context.Background()

			result, err := r.Reconcile(ctx, requestFor(tc.nodeName))
			require.NoError(t, err)

			if tc.expectRequeue {
				assert.Equal(t, defaultRequeueAfter, result.RequeueAfter, "should requeue")
			} else {
				assert.Equal(t, time.Duration(0), result.RequeueAfter, "should not requeue")
			}

			if tc.expectRemoved {
				annotations := getNodeAnnotations(t, rawClient, tc.nodeName)
				assert.NotContains(t, annotations, cyclopsManagedAnnotation, "managed annotation should be removed")
				assert.NotContains(t, annotations, clusterAutoscalerScaleDownDisabledAnnotation, "scale-down-disabled annotation should be removed")
			}

			if tc.expectUnchanged && tc.nodeName != "nonexistent" {
				// Verify annotations are unchanged by reading from the rawClient.
				node, err := rawClient.CoreV1().Nodes().Get(ctx, tc.nodeName, metav1.GetOptions{})
				if err == nil {
					// Find the original node to compare annotations.
					for _, obj := range tc.objects {
						if n, ok := obj.(*corev1.Node); ok && n.Name == tc.nodeName {
							assert.Equal(t, n.Annotations, node.Annotations, "annotations should be unchanged")
							break
						}
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Metric helpers
// ---------------------------------------------------------------------------

// getCounterValue reads the current value of a prometheus Counter.
func getCounterValue(c interface{ Write(*dto.Metric) error }) float64 {
	var m dto.Metric
	_ = c.Write(&m)
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	return 0
}

// resetCleanupMetrics resets the node controller cleanup metrics so tests don't interfere
// with each other.
func resetCleanupMetrics() {
	metrics.NodeCleanupReconciles.Reset()
	// NodeCleanupAnnotationsRemoved is a plain Counter and cannot be reset,
	// so we read its value before/after instead.
}

// ---------------------------------------------------------------------------
// Metric tests
// ---------------------------------------------------------------------------

func TestReconcile_Metrics(t *testing.T) {
	t.Run("cleaned emits cleaned metrics", func(t *testing.T) {
		resetCleanupMetrics()

		node := testNode("node-m1", workerLabels, bothAnnotations())
		ng := testNodeGroup("ng-m1", workerLabels)
		r, _ := newTestReconciler(node, ng)

		// Snapshot the counter before reconcile.
		removedBefore := getCounterValue(metrics.NodeCleanupAnnotationsRemoved)

		_, err := r.Reconcile(context.Background(), requestFor("node-m1"))
		require.NoError(t, err)

		assert.Equal(t, float64(1), getCounterValue(metrics.NodeCleanupReconciles.WithLabelValues("cleaned")),
			"cleaned reconcile counter should be 1")
		assert.Equal(t, removedBefore+1, getCounterValue(metrics.NodeCleanupAnnotationsRemoved),
			"annotations removed counter should increment by 1")
	})

	t.Run("active CNR emits active_cnr_skipped metric", func(t *testing.T) {
		resetCleanupMetrics()

		node := testNode("node-m2", workerLabels, bothAnnotations())
		ng := testNodeGroup("ng-m2", workerLabels)
		cnr := testCNR("cnr-m2", atlassianv1.CycleNodeRequestScalingUp, workerLabels)
		r, _ := newTestReconciler(node, ng, cnr)

		_, err := r.Reconcile(context.Background(), requestFor("node-m2"))
		require.NoError(t, err)

		assert.Equal(t, float64(1), getCounterValue(metrics.NodeCleanupReconciles.WithLabelValues("active_cnr_skipped")),
			"active_cnr_skipped reconcile counter should be 1")
	})

	t.Run("no nodegroup emits no_nodegroup_skipped metric", func(t *testing.T) {
		resetCleanupMetrics()

		node := testNode("node-m3", workerLabels, bothAnnotations())
		// No NodeGroup created.
		r, _ := newTestReconciler(node)

		_, err := r.Reconcile(context.Background(), requestFor("node-m3"))
		require.NoError(t, err)

		assert.Equal(t, float64(1), getCounterValue(metrics.NodeCleanupReconciles.WithLabelValues("no_nodegroup_skipped")),
			"no_nodegroup_skipped reconcile counter should be 1")
	})
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

func TestReconcile_RequeueThenCleanup(t *testing.T) {
	node := testNode("node-1", workerLabels, bothAnnotations())
	ng := testNodeGroup("ng-1", workerLabels)
	cnr := testCNR("cnr-1", atlassianv1.CycleNodeRequestScalingUp, workerLabels)

	r, rawClient := newTestReconciler(node, ng, cnr)
	ctx := context.Background()

	// First reconcile: active CNR exists, should requeue.
	result, err := r.Reconcile(ctx, requestFor("node-1"))
	require.NoError(t, err)
	assert.Equal(t, defaultRequeueAfter, result.RequeueAfter, "first reconcile should requeue")

	annotations := getNodeAnnotations(t, rawClient, "node-1")
	assert.Contains(t, annotations, cyclopsManagedAnnotation, "annotations should still be present after requeue")

	// Simulate CNR deletion by removing it from the fake client.
	require.NoError(t, r.client.Delete(ctx, cnr))

	// Second reconcile: no active CNR, should clean up.
	result, err = r.Reconcile(ctx, requestFor("node-1"))
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), result.RequeueAfter, "second reconcile should not requeue")

	annotations = getNodeAnnotations(t, rawClient, "node-1")
	assert.NotContains(t, annotations, cyclopsManagedAnnotation, "managed annotation should be removed")
	assert.NotContains(t, annotations, clusterAutoscalerScaleDownDisabledAnnotation, "scale-down-disabled annotation should be removed")
}

// TestReconcile_PartialRemovalRecovery verifies the node controller cleans up
// an orphaned marker annotation when a previous removal attempt succeeded for
// the CA annotation but failed for the marker. This is a regression test —
// an earlier version required both annotations to be present before acting,
// which left the orphaned marker permanently stuck.
func TestReconcile_PartialRemovalRecovery(t *testing.T) {
	// Node has only the marker — simulates a prior call to
	// RemoveScaleDownDisabledAnnotationsFromNode that removed the CA
	// annotation but failed on the marker.
	node := testNode("node-partial", workerLabels, map[string]string{
		cyclopsManagedAnnotation: "true",
	})
	ng := testNodeGroup("ng-1", workerLabels)

	r, rawClient := newTestReconciler(node, ng)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, requestFor("node-partial"))
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), result.RequeueAfter, "should not requeue")

	annotations := getNodeAnnotations(t, rawClient, "node-partial")
	assert.NotContains(t, annotations, cyclopsManagedAnnotation,
		"orphaned marker annotation must be cleaned up even when CA annotation is already gone")
}

func TestReconcile_MultipleNodesMixedState(t *testing.T) {
	// node-clean: both annotations + nodegroup + no CNR → should be cleaned
	nodeClean := testNode("node-clean", workerLabels, bothAnnotations())
	// node-active: both annotations + nodegroup + active CNR → should requeue
	nodeActive := testNode("node-active", workerLabels, bothAnnotations())
	// node-no-ng: both annotations + no nodegroup → untouched
	infraLabels := map[string]string{"node-role": "infra"}
	nodeNoNG := testNode("node-no-ng", infraLabels, bothAnnotations())
	// node-plain: no annotations + nodegroup → untouched
	nodePlain := testNode("node-plain", workerLabels, nil)

	ng := testNodeGroup("ng-1", workerLabels)
	cnr := testCNR("cnr-1", atlassianv1.CycleNodeRequestScalingUp, workerLabels)

	r, rawClient := newTestReconciler(nodeClean, nodeActive, nodeNoNG, nodePlain, ng, cnr)
	ctx := context.Background()

	// Reconcile node-clean: no active CNR matches (the CNR matches worker labels,
	// but node-clean also has worker labels, so it IS matched by the CNR).
	// Actually, node-clean does match the CNR, so it should requeue.
	result, err := r.Reconcile(ctx, requestFor("node-clean"))
	require.NoError(t, err)
	assert.Equal(t, defaultRequeueAfter, result.RequeueAfter, "node-clean should requeue while CNR is active")

	// Delete the CNR so node-clean can be cleaned up.
	require.NoError(t, r.client.Delete(ctx, cnr))

	result, err = r.Reconcile(ctx, requestFor("node-clean"))
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
	annotations := getNodeAnnotations(t, rawClient, "node-clean")
	assert.NotContains(t, annotations, cyclopsManagedAnnotation, "node-clean should be cleaned up")

	// Reconcile node-no-ng: has annotations but no nodegroup → untouched.
	result, err = r.Reconcile(ctx, requestFor("node-no-ng"))
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
	annotations = getNodeAnnotations(t, rawClient, "node-no-ng")
	assert.Contains(t, annotations, cyclopsManagedAnnotation, "node-no-ng should keep annotations")

	// Reconcile node-plain: no annotations → early return, untouched.
	result, err = r.Reconcile(ctx, requestFor("node-plain"))
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
}
