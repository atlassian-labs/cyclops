package k8s

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// CordonNode performs a patch operation on a node to mark it as unschedulable
func CordonNode(name string, client kubernetes.Interface) error {
	patches := []Patch{
		{
			Op:    "add",
			Path:  "/spec/unschedulable",
			Value: true,
		},
	}
	return PatchNode(name, patches, client)
}

// UncordonNode performs a patch operation on a node to mark it as schedulable
func UncordonNode(name string, client kubernetes.Interface) error {
	patches := []Patch{
		{
			Op:    "add",
			Path:  "/spec/unschedulable",
			Value: false,
		},
	}
	return PatchNode(name, patches, client)
}

// IsCordoned checks if a node is cordoned
func IsCordoned(name string, client kubernetes.Interface) (bool, error) {
	node, err := client.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return node.Spec.Unschedulable, nil
}

// AddLabelToNode performs a patch operation on a node to add a label to the node
func AddLabelToNode(nodeName string, labelName string, labelValue string, client kubernetes.Interface) error {
	patches := []Patch{
		{
			Op: "add",
			// json patch spec maps "~1" to "/" as an escape sequence, so we do the translation here...
			Path:  fmt.Sprintf("/metadata/labels/%s", strings.Replace(labelName, "/", "~1", -1)),
			Value: labelValue,
		},
	}
	return PatchNode(nodeName, patches, client)
}

// NodeLister defines an object that can list nodes with a label selector
type NodeLister interface {
	List(labels.Selector) ([]*v1.Node, error)
}

// cachedNodeList uses an indexer cache to list nodes from memory
type cachedNodeList struct {
	cache cache.Indexer
}

// NewCachedNodeList creates a new cachedNodeList
func NewCachedNodeList(cache cache.Indexer) NodeLister {
	return &cachedNodeList{cache: cache}
}

// List nodes with selector from cache
func (c *cachedNodeList) List(selector labels.Selector) ([]*v1.Node, error) {
	var nodes []*v1.Node

	err := cache.ListAll(c.cache, selector, func(v interface{}) {
		if n, ok := v.(*v1.Node); ok {
			nodes = append(nodes, n)
		}
	})

	return nodes, err
}
