package v1

import (
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// NodeLabelSelector converts a metav1.LabelSelector to a labels.Selector
func (in *CycleNodeRequest) NodeLabelSelector() (labels.Selector, error) {
	return metaV1.LabelSelectorAsSelector(&in.Spec.Selector)
}
