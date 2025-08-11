package transitioner

import (
	"reflect"
	"testing"
	"time"

	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
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
