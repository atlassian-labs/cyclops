package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// newNodeForPatch creates a minimal node and registers it with a fresh fake
// clientset, returning both. Using fake.NewSimpleClientset means patch calls
// go through the object tracker and work out of the box.
func newNodeForPatch(name string, annotations, labels map[string]string) (*corev1.Node, *fake.Clientset) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
			Labels:      labels,
		},
	}
	client := fake.NewSimpleClientset(node)
	return node, client
}

// TestAddAnnotationToNode_WithExistingAnnotations verifies that
// AddAnnotationToNode merges into an existing annotations map without
// clobbering other entries.
func TestAddAnnotationToNode_WithExistingAnnotations(t *testing.T) {
	node, client := newNodeForPatch("test-node",
		map[string]string{"existing-key": "existing-value"},
		nil,
	)

	err := AddAnnotationToNode(node.Name, "new-key", "new-value", client)
	require.NoError(t, err)

	got, err := client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "existing-value", got.Annotations["existing-key"], "existing annotation should be preserved")
	assert.Equal(t, "new-value", got.Annotations["new-key"], "new annotation should be set")
}

// TestAddAnnotationToNode_WithNilAnnotations verifies that AddAnnotationToNode
// succeeds even when the node's annotations map is nil. This was the bug with
// the old JSON Patch "add" approach: adding a key to a nil map via JSON Patch
// fails at the apiserver level.
func TestAddAnnotationToNode_WithNilAnnotations(t *testing.T) {
	node, client := newNodeForPatch("test-node", nil, nil)

	err := AddAnnotationToNode(node.Name, "some-key", "some-value", client)
	require.NoError(t, err)

	got, err := client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "some-value", got.Annotations["some-key"])
}

// TestAddAnnotationToNode_OverwritesExistingValue verifies that patching an
// annotation that already exists updates its value.
func TestAddAnnotationToNode_OverwritesExistingValue(t *testing.T) {
	node, client := newNodeForPatch("test-node",
		map[string]string{"my-key": "old-value"},
		nil,
	)

	err := AddAnnotationToNode(node.Name, "my-key", "new-value", client)
	require.NoError(t, err)

	got, err := client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "new-value", got.Annotations["my-key"])
}

// TestAddLabelToNode_WithExistingLabels verifies that AddLabelToNode merges
// into an existing labels map without clobbering other entries.
func TestAddLabelToNode_WithExistingLabels(t *testing.T) {
	node, client := newNodeForPatch("test-node",
		nil,
		map[string]string{"existing-label": "existing-value"},
	)

	err := AddLabelToNode(node.Name, "new-label", "new-value", client)
	require.NoError(t, err)

	got, err := client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "existing-value", got.Labels["existing-label"], "existing label should be preserved")
	assert.Equal(t, "new-value", got.Labels["new-label"], "new label should be set")
}

// TestAddLabelToNode_WithNilLabels verifies that AddLabelToNode succeeds even
// when the node has no labels. Mirrors the nil-annotations case.
func TestAddLabelToNode_WithNilLabels(t *testing.T) {
	node, client := newNodeForPatch("test-node", nil, nil)

	err := AddLabelToNode(node.Name, "role", "worker", client)
	require.NoError(t, err)

	got, err := client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "worker", got.Labels["role"])
}

// TestAddAnnotationAndLabelToNode_Independent verifies that patching
// annotations and labels independently doesn't interfere.
func TestAddAnnotationAndLabelToNode_Independent(t *testing.T) {
	node, client := newNodeForPatch("test-node", nil, nil)

	require.NoError(t, AddAnnotationToNode(node.Name, "ann", "ann-val", client))
	require.NoError(t, AddLabelToNode(node.Name, "lbl", "lbl-val", client))

	got, err := client.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "ann-val", got.Annotations["ann"])
	assert.Equal(t, "lbl-val", got.Labels["lbl"])
}

// TestAddAnnotationToNode_NodeNotFound verifies that an error is returned when
// the target node doesn't exist.
func TestAddAnnotationToNode_NodeNotFound(t *testing.T) {
	client := fake.NewSimpleClientset() // empty — no nodes registered
	err := AddAnnotationToNode("does-not-exist", "k", "v", client)
	assert.Error(t, err)
}

// TestAddLabelToNode_NodeNotFound verifies that an error is returned when the
// target node doesn't exist.
func TestAddLabelToNode_NodeNotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	err := AddLabelToNode("does-not-exist", "k", "v", client)
	assert.Error(t, err)
}
