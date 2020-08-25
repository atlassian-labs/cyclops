package transitioner

import (
	"testing"

	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
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
		knodes                 []corev1.Node
		cnodes                 map[string]cloudprovider.Instance
		expectNotInCPNodeGroup []string
		expectNotInKube        []string
	}{
		{
			"kube nodes match cloud provider nodes",
			[]corev1.Node{
				buildNode(dummyInstanceA),
				buildNode(dummyInstanceB),
				buildNode(dummyInstanceC),
			},
			map[string]cloudprovider.Instance{
				dummyInstanceA.providerID: &dummyInstanceA,
				dummyInstanceB.providerID: &dummyInstanceB,
				dummyInstanceC.providerID: &dummyInstanceC,
			},
			[]string{},
			[]string{},
		},
		{
			"more nodes in kube than cloud provider",
			[]corev1.Node{
				buildNode(dummyInstanceA),
				buildNode(dummyInstanceB),
				buildNode(dummyInstanceC),
			},
			map[string]cloudprovider.Instance{
				dummyInstanceA.providerID: &dummyInstanceA,
				dummyInstanceB.providerID: &dummyInstanceB,
			},
			[]string{"id \"aws:///us-east-1c/i-cbcdefghijk\""},
			[]string{},
		},
		{
			"more nodes in cloud provider than kube",
			[]corev1.Node{
				buildNode(dummyInstanceA),
				buildNode(dummyInstanceB),
			},
			map[string]cloudprovider.Instance{
				dummyInstanceA.providerID: &dummyInstanceA,
				dummyInstanceB.providerID: &dummyInstanceB,
				dummyInstanceC.providerID: &dummyInstanceC,
			},
			[]string{},
			[]string{"id \"aws:///us-east-1c/i-cbcdefghijk\" in \"GroupC\""},
		},
		{
			"no nodes in cloud provider",
			[]corev1.Node{
				buildNode(dummyInstanceA),
				buildNode(dummyInstanceB),
			},
			map[string]cloudprovider.Instance{},
			[]string{"id \"aws:///us-east-1a/i-abcdefghijk\"", "id \"aws:///us-east-1b/i-bbcdefghijk\""},
			[]string{},
		},
		{
			"no nodes in kube",
			[]corev1.Node{},
			map[string]cloudprovider.Instance{
				dummyInstanceA.providerID: &dummyInstanceA,
				dummyInstanceB.providerID: &dummyInstanceB,
			},
			[]string{},
			[]string{"id \"aws:///us-east-1a/i-abcdefghijk\" in \"GroupA\"", "id \"aws:///us-east-1b/i-bbcdefghijk\" in \"GroupB\""},
		},
		{
			"both cloud provider and kube nodes are empty",
			[]corev1.Node{},
			map[string]cloudprovider.Instance{},
			[]string{},
			[]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nodesNotInCPNodeGroup, nodesNotInKube := findOffendingNodes(test.knodes, test.cnodes)
			assert.ElementsMatch(t, test.expectNotInCPNodeGroup, nodesNotInCPNodeGroup)
			assert.ElementsMatch(t, test.expectNotInKube, nodesNotInKube)
		})
	}
}

func buildNode(n dummyInstance) corev1.Node {
	return corev1.Node{
		Spec: corev1.NodeSpec{ProviderID: n.providerID},
	}
}
