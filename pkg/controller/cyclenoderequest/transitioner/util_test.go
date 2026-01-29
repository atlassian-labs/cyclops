package transitioner

import (
	"context"
	"reflect"
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/atlassian-labs/cyclops/pkg/mock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type dummyInstance struct {
	providerID string
	nodeGroup  string
}

func (i *dummyInstance) ID() string {
	return i.providerID
}

func (i *dummyInstance) OutOfDate() bool {
	return false
}

func (i *dummyInstance) MatchesProviderID(string) bool {
	return true
}

func (i *dummyInstance) NodeGroupName() string {
	return i.nodeGroup
}

func TestFindOffendingNodes(t *testing.T) {
	dummyInstanceA := dummyInstance{
		providerID: "aws:///us-east-1a/i-abcdefghijk",
		nodeGroup:  "GroupA",
	}
	dummyInstanceB := dummyInstance{
		providerID: "aws:///us-east-1b/i-bbcdefghijk",
		nodeGroup:  "GroupB",
	}
	dummyInstanceC := dummyInstance{
		providerID: "aws:///us-east-1c/i-cbcdefghijk",
		nodeGroup:  "GroupC",
	}

	tests := []struct {
		name                   string
		knodes                 map[string]corev1.Node
		cnodes                 map[string]cloudprovider.Instance
		expectNotInCPNodeGroup map[string]corev1.Node
		expectNotInKube        map[string]cloudprovider.Instance
	}{
		{
			"kube nodes match cloud provider nodes",
			map[string]corev1.Node{
				dummyInstanceA.providerID: buildNode(dummyInstanceA),
				dummyInstanceB.providerID: buildNode(dummyInstanceB),
				dummyInstanceC.providerID: buildNode(dummyInstanceC),
			},
			map[string]cloudprovider.Instance{
				dummyInstanceA.providerID: &dummyInstanceA,
				dummyInstanceB.providerID: &dummyInstanceB,
				dummyInstanceC.providerID: &dummyInstanceC,
			},
			make(map[string]corev1.Node),
			make(map[string]cloudprovider.Instance),
		},
		{
			"more nodes in kube than cloud provider",
			map[string]corev1.Node{
				dummyInstanceA.providerID: buildNode(dummyInstanceA),
				dummyInstanceB.providerID: buildNode(dummyInstanceB),
				dummyInstanceC.providerID: buildNode(dummyInstanceC),
			},
			map[string]cloudprovider.Instance{
				dummyInstanceA.providerID: &dummyInstanceA,
				dummyInstanceB.providerID: &dummyInstanceB,
			},
			map[string]corev1.Node{
				dummyInstanceC.providerID: buildNode(dummyInstanceC),
			},
			make(map[string]cloudprovider.Instance),
		},
		{
			"more nodes in cloud provider than kube",
			map[string]corev1.Node{
				dummyInstanceA.providerID: buildNode(dummyInstanceA),
				dummyInstanceB.providerID: buildNode(dummyInstanceB),
			},
			map[string]cloudprovider.Instance{
				dummyInstanceA.providerID: &dummyInstanceA,
				dummyInstanceB.providerID: &dummyInstanceB,
				dummyInstanceC.providerID: &dummyInstanceC,
			},
			make(map[string]corev1.Node),
			map[string]cloudprovider.Instance{
				dummyInstanceC.providerID: &dummyInstanceC,
			},
		},
		{
			"no nodes in cloud provider",
			map[string]corev1.Node{
				dummyInstanceA.providerID: buildNode(dummyInstanceA),
				dummyInstanceB.providerID: buildNode(dummyInstanceB),
			},
			make(map[string]cloudprovider.Instance),
			map[string]corev1.Node{
				dummyInstanceA.providerID: buildNode(dummyInstanceA),
				dummyInstanceB.providerID: buildNode(dummyInstanceB),
			},
			make(map[string]cloudprovider.Instance),
		},
		{
			"no nodes in kube",
			make(map[string]corev1.Node),
			map[string]cloudprovider.Instance{
				dummyInstanceA.providerID: &dummyInstanceA,
				dummyInstanceB.providerID: &dummyInstanceB,
			},
			make(map[string]corev1.Node),
			map[string]cloudprovider.Instance{
				dummyInstanceA.providerID: &dummyInstanceA,
				dummyInstanceB.providerID: &dummyInstanceB,
			},
		},
		{
			"both cloud provider and kube nodes are empty",
			make(map[string]corev1.Node),
			make(map[string]cloudprovider.Instance),
			make(map[string]corev1.Node),
			make(map[string]cloudprovider.Instance),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nodesNotInCPNodeGroup, nodesNotInKube := findProblemNodes(test.knodes, test.cnodes)
			assert.Equal(t, true, reflect.DeepEqual(test.expectNotInCPNodeGroup, nodesNotInCPNodeGroup))
			assert.Equal(t, true, reflect.DeepEqual(test.expectNotInKube, nodesNotInKube))
		})
	}
}

func TestCountNodesCreatedAfter(t *testing.T) {
	baseTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		nodes         map[string]corev1.Node
		cutoffTime    time.Time
		expectedCount int
	}{
		{
			name:          "empty nodes map",
			nodes:         map[string]corev1.Node{},
			cutoffTime:    baseTime,
			expectedCount: 0,
		},
		{
			name: "all nodes created after cutoff",
			nodes: map[string]corev1.Node{
				"node1": buildNodeWithTimestamp("node1", baseTime.Add(1*time.Hour)),
				"node2": buildNodeWithTimestamp("node2", baseTime.Add(1*time.Hour)),
				"node3": buildNodeWithTimestamp("node3", baseTime.Add(1*time.Hour)),
			},
			cutoffTime:    baseTime,
			expectedCount: 3,
		},
		{
			name: "all nodes created before cutoff",
			nodes: map[string]corev1.Node{
				"node1": buildNodeWithTimestamp("node1", baseTime.Add(-1*time.Hour)),
				"node2": buildNodeWithTimestamp("node2", baseTime.Add(-1*time.Hour)),
				"node3": buildNodeWithTimestamp("node3", baseTime.Add(-1*time.Hour)),
			},
			cutoffTime:    baseTime,
			expectedCount: 0,
		},
		{
			name: "mixed nodes before and after cutoff",
			nodes: map[string]corev1.Node{
				"node1": buildNodeWithTimestamp("node1", baseTime.Add(-1*time.Hour)),
				"node2": buildNodeWithTimestamp("node2", baseTime.Add(1*time.Hour)),
				"node3": buildNodeWithTimestamp("node3", baseTime.Add(1*time.Hour)),
			},
			cutoffTime:    baseTime,
			expectedCount: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := countNodesCreatedAfter(test.nodes, test.cutoffTime)
			assert.Equal(t, test.expectedCount, result)
		})
	}
}

func buildNode(n dummyInstance) corev1.Node {
	return corev1.Node{
		Spec: corev1.NodeSpec{ProviderID: n.providerID},
	}
}

func buildNodeWithTimestamp(name string, timestamp time.Time) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(timestamp),
		},
	}
}

// TestShouldManageAnnotations tests the shouldManageAnnotations function with different NodeGroup annotation values
func TestShouldManageAnnotations(t *testing.T) {
	tests := []struct {
		name                string
		nodeGroupAnnotation map[string]string // NodeGroup annotations
		expectedResult      bool              // Expected return value
		description         string
	}{
		{
			name:                "no annotation - default enabled",
			nodeGroupAnnotation: nil,
			expectedResult:      true,
			description:         "When NodeGroup has no annotation, annotations should be managed (default enabled)",
		},
		{
			name:                "annotation missing - default enabled",
			nodeGroupAnnotation: map[string]string{},
			expectedResult:      true,
			description:         "When NodeGroup annotation is missing, annotations should be managed (default enabled)",
		},
		{
			name:                "annotation value false - enabled",
			nodeGroupAnnotation: map[string]string{"cyclops.atlassian.com/disable-annotation-management": "false"},
			expectedResult:      true,
			description:         "When annotation value is 'false', annotations should be managed",
		},
		{
			name:                "annotation value true - opt-out disabled",
			nodeGroupAnnotation: map[string]string{"cyclops.atlassian.com/disable-annotation-management": "true"},
			expectedResult:      false,
			description:         "When annotation value is 'true', annotations should NOT be managed (opt-out)",
		},
		{
			name:                "annotation value disabled - opt-out disabled",
			nodeGroupAnnotation: map[string]string{"cyclops.atlassian.com/disable-annotation-management": "disabled"},
			expectedResult:      true, // "disabled" is not recognized, defaults to enabled
			description:         "When annotation value is 'disabled' (not recognized), defaults to enabled",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Create a CNR
			cnr := &v1.CycleNodeRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cnr",
					Namespace: "kube-system",
				},
				Spec: v1.CycleNodeRequestSpec{
					NodeGroupsList: []string{"ng-1"},
				},
			}

			// Create a NodeGroup with the test annotation
			nodeGroup := &v1.NodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "ng-1",
					Annotations: test.nodeGroupAnnotation,
				},
				Spec: v1.NodeGroupSpec{
					NodeGroupName: "ng-1",
				},
			}

			// Create fake transitioner with NodeGroup
			fakeTransitioner := NewFakeTransitioner(cnr,
				WithExtraKubeObject(nodeGroup),
			)

			// Test shouldManageAnnotations
			result := fakeTransitioner.shouldManageAnnotations()

			assert.Equal(t, test.expectedResult, result, test.description)
		})
	}
}

// TestShouldManageAnnotationsNoNodeGroup tests behavior when NodeGroup is not found
func TestShouldManageAnnotationsNoNodeGroup(t *testing.T) {
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cnr",
			Namespace: "kube-system",
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupsList: []string{"non-existent-ng"},
		},
	}

	// Create fake transitioner WITHOUT NodeGroup
	fakeTransitioner := NewFakeTransitioner(cnr)

	// Should default to enabled when NodeGroup not found
	result := fakeTransitioner.shouldManageAnnotations()
	assert.True(t, result, "When NodeGroup is not found, should default to enabled")
}

// TestAnnotationNotAddedWhenOptOut tests that annotations are NOT added to nodes when opt-out is enabled
func TestAnnotationNotAddedWhenOptOut(t *testing.T) {
	// Create a NodeGroup with opt-out enabled
	nodeGroup := &v1.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ng-1",
			Annotations: map[string]string{
				"cyclops.atlassian.com/disable-annotation-management": "true",
			},
		},
		Spec: v1.NodeGroupSpec{
			NodeGroupName: "ng-1",
		},
	}

	// Create nodes
	nodegroup, err := mock.NewNodegroup("ng-1", 1)
	assert.NoError(t, err)

	// Create CNR in ScalingUp phase with ScaleUpStarted timestamp
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cnr",
			Namespace: "kube-system",
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupsList: []string{"ng-1"},
		},
		Status: v1.CycleNodeRequestStatus{
			Phase: v1.CycleNodeRequestScalingUp,
			ScaleUpStarted: &metav1.Time{
				Time: time.Now().Add(-1 * time.Minute),
			},
		},
	}

	// Create fake transitioner with NodeGroup and nodes
	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
		WithExtraKubeObject(nodeGroup),
	)

	// Verify opt-out is detected
	assert.False(t, fakeTransitioner.shouldManageAnnotations(), "Opt-out should be detected")

	// Try to add annotation to a new node (simulating transitionScalingUp behavior)
	newNodeName := nodegroup[0].Name
	err = fakeTransitioner.addScaleDownDisabledAnnotation(newNodeName)
	assert.NoError(t, err, "addScaleDownDisabledAnnotation should not error even when opt-out")

	// Verify annotation was NOT added to the node
	node, err := fakeTransitioner.rm.RawClient.CoreV1().Nodes().Get(
		context.TODO(), newNodeName, metav1.GetOptions{},
	)
	assert.NoError(t, err)

	// Check that the cluster-autoscaler annotation does NOT exist
	annotationValue, exists := node.Annotations["cluster-autoscaler.kubernetes.io/scale-down-disabled"]
	assert.False(t, exists, "Annotation should NOT exist when opt-out is enabled")
	assert.Empty(t, annotationValue, "Annotation value should be empty when opt-out is enabled")
}

// TestAnnotationAddedWhenNotExists tests Scenario 1: Cyclops adds both annotations when cluster-autoscaler annotation doesn't exist
func TestAnnotationAddedWhenNotExists(t *testing.T) {
	// Create a NodeGroup without opt-out (default enabled)
	nodeGroup := &v1.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "ng-1",
			Annotations: map[string]string{}, // No opt-out annotation
		},
		Spec: v1.NodeGroupSpec{
			NodeGroupName: "ng-1",
		},
	}

	// Create nodes
	nodegroup, err := mock.NewNodegroup("ng-1", 1)
	assert.NoError(t, err)

	// Create CNR
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cnr",
			Namespace: "kube-system",
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupsList: []string{"ng-1"},
		},
	}

	// Create fake transitioner with NodeGroup and nodes
	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
		WithExtraKubeObject(nodeGroup),
	)

	// Verify annotation management is enabled
	assert.True(t, fakeTransitioner.shouldManageAnnotations(), "Annotation management should be enabled")

	// Add annotation to a node (simulating transitionScalingUp behavior)
	newNodeName := nodegroup[0].Name
	err = fakeTransitioner.addScaleDownDisabledAnnotation(newNodeName)
	assert.NoError(t, err, "addScaleDownDisabledAnnotation should succeed")

	// Verify both annotations were added to the node
	node, err := fakeTransitioner.rm.RawClient.CoreV1().Nodes().Get(
		context.TODO(), newNodeName, metav1.GetOptions{},
	)
	assert.NoError(t, err)

	// Check that the cluster-autoscaler annotation exists with value "true"
	annotationValue, exists := node.Annotations["cluster-autoscaler.kubernetes.io/scale-down-disabled"]
	assert.True(t, exists, "Cluster Autoscaler annotation should exist")
	assert.Equal(t, "true", annotationValue, "Cluster Autoscaler annotation should have value 'true'")

	// Check that the marker annotation exists
	markerValue, markerExists := node.Annotations["cyclops.atlassian.com/annotation-managed"]
	assert.True(t, markerExists, "Marker annotation should exist")
	assert.Equal(t, "true", markerValue, "Marker annotation should have value 'true'")
}

// TestAnnotationPreservedWhenExists tests Scenario 2: Cyclops preserves existing annotation and doesn't add marker
func TestAnnotationPreservedWhenExists(t *testing.T) {
	// Create a NodeGroup without opt-out (default enabled)
	nodeGroup := &v1.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "ng-1",
			Annotations: map[string]string{}, // No opt-out annotation
		},
		Spec: v1.NodeGroupSpec{
			NodeGroupName: "ng-1",
		},
	}

	// Create nodes
	nodegroup, err := mock.NewNodegroup("ng-1", 1)
	assert.NoError(t, err)

	// Create CNR
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cnr",
			Namespace: "kube-system",
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupsList: []string{"ng-1"},
		},
	}

	// Create fake transitioner with NodeGroup and nodes
	fakeTransitioner := NewFakeTransitioner(cnr,
		WithKubeNodes(nodegroup),
		WithCloudProviderInstances(nodegroup),
		WithExtraKubeObject(nodeGroup),
	)

	// Manually add the cluster-autoscaler annotation to simulate ASG setting it
	// We need to add it to the Kubernetes node before Cyclops tries to add it
	newNodeName := nodegroup[0].Name
	node, err := fakeTransitioner.rm.RawClient.CoreV1().Nodes().Get(
		context.TODO(), newNodeName, metav1.GetOptions{},
	)
	assert.NoError(t, err)

	// Add pre-existing annotation to simulate ASG Launch Template
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations["cluster-autoscaler.kubernetes.io/scale-down-disabled"] = "true"
	_, err = fakeTransitioner.rm.RawClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	assert.NoError(t, err, "Should be able to update node with pre-existing annotation")

	// Verify annotation management is enabled
	assert.True(t, fakeTransitioner.shouldManageAnnotations(), "Annotation management should be enabled")

	// Try to add annotation to the node (should preserve existing)
	err = fakeTransitioner.addScaleDownDisabledAnnotation(newNodeName)
	assert.NoError(t, err, "addScaleDownDisabledAnnotation should succeed even when annotation exists")

	// Verify the existing annotation was preserved
	node, err = fakeTransitioner.rm.RawClient.CoreV1().Nodes().Get(
		context.TODO(), newNodeName, metav1.GetOptions{},
	)
	assert.NoError(t, err)

	// Check that the cluster-autoscaler annotation still exists with value "true"
	annotationValue, exists := node.Annotations["cluster-autoscaler.kubernetes.io/scale-down-disabled"]
	assert.True(t, exists, "Cluster Autoscaler annotation should still exist")
	assert.Equal(t, "true", annotationValue, "Cluster Autoscaler annotation should have value 'true'")

	// Check that the marker annotation was NOT added (Cyclops didn't add the annotation, so no marker)
	markerValue, markerExists := node.Annotations["cyclops.atlassian.com/annotation-managed"]
	assert.False(t, markerExists, "Marker annotation should NOT exist when Cyclops preserves existing annotation")
	assert.Empty(t, markerValue, "Marker annotation value should be empty")
}
