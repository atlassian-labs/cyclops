package generation

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
)

const (
	generateExample                       = "xxxxx"
	concurrencyLessThanZeroMessage        = "concurrency cannot be less than 0"
	concurrencyEqualsZeroMessage          = "concurrency set to 0"
	nodeGroupScaledToZeroMessage          = "node group is scaled to 0"
	cnrNameLabelKey                       = "name"
	cnrReasonAnnotationKey                = "reason"
	cyclingTimeoutLessThanZeroMessage     = "cyclingTimeout cannot be less than 0 seconds"
	cyclingTimeoutNotInTimeDurationFormat = "cyclingTimeout is not in time duration format"
)

// onceShotNodeLister creates a node lister that lists nodes with the controller client.Client as a Get/List
type onceShotNodeLister struct {
	c client.Client
}

// NewOneShotNodeLister creates a new onceShotNodeLister
func NewOneShotNodeLister(c client.Client) k8s.NodeLister {
	return &onceShotNodeLister{c: c}
}

// List lists nodes from the APIServer. Not from a cache
func (o *onceShotNodeLister) List(selector labels.Selector) ([]*corev1.Node, error) {
	var nodeList corev1.NodeList
	if err := o.c.List(context.TODO(), &nodeList, &client.ListOptions{LabelSelector: selector}); err != nil {
		return nil, err
	}
	nodes := make([]*corev1.Node, 0, len(nodeList.Items))
	for i := range nodeList.Items {
		nodes = append(nodes, &nodeList.Items[i])
	}
	return nodes, nil
}

// validateCycleSettings returns if the cycle settings are valid for cycling and why not
func validateCycleSettings(settings atlassianv1.CycleSettings) (bool, string) {
	if settings.Concurrency < 0 {
		return false, concurrencyLessThanZeroMessage
	}

	if settings.Concurrency == 0 {
		return false, concurrencyEqualsZeroMessage
	}

	// CyclingTimeout flag is optional, only validate if not empty
	if settings.CyclingTimeout != nil && settings.CyclingTimeout.Duration < 0*time.Second {
		return false, cyclingTimeoutLessThanZeroMessage
	}

	return true, ""
}

// validateMetadata validates metadata names and labels are valid in k8s for a CNR / NodeGroup
// appends generateExample when using GenerateName
func validateMetadata(meta metav1.ObjectMeta) (bool, string) {
	// append dummy generate name pattern to validate if used
	name := meta.Name
	if name == "" {
		name = meta.GenerateName + generateExample
	}

	if errStrs := validation.IsDNS1123Subdomain(name); len(errStrs) > 0 {
		return false, fmt.Sprint("name is not valid: ", strings.Join(errStrs, ", "))
	}

	if labelValue, ok := meta.Labels[cnrNameLabelKey]; ok {
		if errStrs := validation.IsDNS1123Label(labelValue); len(errStrs) > 0 {
			return false, fmt.Sprint("label value is not valid: ", strings.Join(errStrs, ", "))
		}
	}

	return true, ""
}

// GetName returns the Name or GenerateName of the object Meta
func GetName(meta metav1.ObjectMeta) string {
	if meta.Name == "" {
		return meta.GenerateName
	}
	return meta.Name
}

// GetNameExample returns the Name or GenerateName of the object Meta with an "xxxx" if using GeneratedName
func GetNameExample(meta metav1.ObjectMeta) (string, string) {
	if meta.Name == "" {
		return meta.GenerateName, generateExample
	}
	return meta.Name, ""
}

// validateSelectorWithNodes lists nodes with the nodeLister and determines if the selector is valid and non 0 and optionally matchNodes exist in the nodegroup
func validateSelectorWithNodes(nodeLister k8s.NodeLister, selector labels.Selector, matchNodes []string) (bool, string) {
	nodes, err := nodeLister.List(selector)
	if err != nil {
		return false, fmt.Sprint("failed to list nodes: ", err.Error())
	}

	if len(nodes) == 0 {
		return false, nodeGroupScaledToZeroMessage
	}

	// check that nodes listed as overrides actually exist in the nodegroup
	for _, nodeToFind := range matchNodes {
		var found bool
		for _, node := range nodes {
			if nodeToFind == node.Name {
				found = true
				break
			}
		}

		if !found {
			return false, fmt.Sprintf("the node %q does not exist in the nodegroup but it is specified to cycle", nodeToFind)
		}
	}

	return true, ""
}
