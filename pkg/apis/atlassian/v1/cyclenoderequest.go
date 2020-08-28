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
