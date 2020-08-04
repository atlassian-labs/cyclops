package generation

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
)

// ListNodeGroups list NodeGroupList from ListOptions
func ListNodeGroups(c client.Client, options *client.ListOptions) (*atlassianv1.NodeGroupList, error) {
	var list atlassianv1.NodeGroupList

	err := c.List(context.TODO(), &list, options)
	if err != nil {
		return nil, err
	}

	return &list, nil
}

// GetNodeGroups gets individual node groups and returns the as a list
func GetNodeGroups(c client.Client, names ...string) (*atlassianv1.NodeGroupList, error) {
	var list []atlassianv1.NodeGroup

	for _, name := range names {
		var ng atlassianv1.NodeGroup
		err := c.Get(context.TODO(), client.ObjectKey{Name: name}, &ng)
		if err != nil {
			return nil, err
		}
		list = append(list, ng)
	}

	return &atlassianv1.NodeGroupList{Items: list}, nil
}

// ValidateNodeGroup determines if a nodegroup should be considered for rotation to or not, and if so why not
func ValidateNodeGroup(nodeLister k8s.NodeLister, nodegroup atlassianv1.NodeGroup) (bool, string) {
	if ok, reason := validateMetadata(nodegroup.ObjectMeta); !ok {
		return ok, reason
	}

	if ok, reason := validateCycleSettings(nodegroup.Spec.CycleSettings); !ok {
		return ok, reason
	}

	// validate against nodes in api
	selector, err := metav1.LabelSelectorAsSelector(&nodegroup.Spec.NodeSelector)
	if err != nil {
		return false, fmt.Sprint("failed to parse node label selectors: ", err.Error())
	}

	return validateSelectorWithNodes(nodeLister, selector, nil)
}
