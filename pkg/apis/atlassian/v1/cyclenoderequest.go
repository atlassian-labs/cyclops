package v1

import (
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// NodeLabelSelector converts a metav1.LabelSelector to a labels.Selector
func (in *CycleNodeRequest) NodeLabelSelector() (labels.Selector, error) {
	return metaV1.LabelSelectorAsSelector(&in.Spec.Selector)
}

// GetNodeGroupNames gets a union of cloud provider node group names
// based on CycleNodeRequestSpec `NodeGroupName` and `NodeGroupsList`
func (in *CycleNodeRequest) GetNodeGroupNames() []string {
	return buildNodeGroupNames(in.Spec.NodeGroupsList, in.Spec.NodeGroupName)
}

// IsPartOfNodeGroup returns whether the CycleNodeRequest is part of the
// provided NodeGroup by comparing the list of named cloud provider nodegroups
// defined in each one. Ordering does not affect equality.
func (in *CycleNodeRequest) IsFromNodeGroup(nodegroup NodeGroup) bool {
	return sameNodeGroups(
		buildNodeGroupNames(in.Spec.NodeGroupsList, in.Spec.NodeGroupName),
		nodegroup.GetNodeGroupNames(),
	)
}

// IsFromSameNodeGroup returns whether the CycleNodeRequest is part of the
// same Nodegroup provided as the provided CycleNodeRequest by comparing the
// list of named cloud provider nodegroups defined in each one. Ordering does
// not affect equality.
func (in *CycleNodeRequest) IsFromSameNodeGroup(cnr CycleNodeRequest) bool {
	return sameNodeGroups(
		buildNodeGroupNames(in.Spec.NodeGroupsList, in.Spec.NodeGroupName),
		cnr.GetNodeGroupNames(),
	)
}
