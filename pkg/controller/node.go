package controller

import (
	"context"
	"time"

	"github.com/atlassian-labs/cyclops/pkg/k8s"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ListNodes lists nodes from Kubernetes, optionally filtered by a selector.
func (rm *ResourceManager) ListNodes(selector labels.Selector) ([]v1.Node, error) {
	// List the nodes
	nodeList := v1.NodeList{}
	listOptions := &client.ListOptions{
		LabelSelector: selector,
	}
	err := rm.Client.List(context.TODO(), &nodeList, listOptions)
	if err != nil {
		return []v1.Node{}, err
	}
	return nodeList.Items, nil
}

// GetNode gets a node object from Kubernetes by name.
func (rm *ResourceManager) GetNode(name string) (*v1.Node, error) {
	// Get the node
	node := &v1.Node{}
	key := types.NamespacedName{
		Namespace: "",
		Name:      name,
	}
	err := rm.Client.Get(context.TODO(), key, node)
	return node, err
}

// DeleteNode deletes a node from Kubernetes by name.
func (rm *ResourceManager) DeleteNode(name string) error {
	// Get the node
	node, err := rm.GetNode(name)
	if err != nil {
		return err
	}

	// Delete the node
	return rm.Client.Delete(context.TODO(), node)
}

// DrainPods drains the pods off the named node.
func (rm *ResourceManager) DrainPods(nodeName string, unhealthyAfter time.Duration) (finished bool, errs []error) {
	// Get drainable pods and drain them
	drainablePods, err := rm.GetDrainablePodsOnNode(nodeName)
	if err != nil {
		return false, []error{err}
	}

	// No pods to drain, finish early
	if len(drainablePods) == 0 {
		return true, errs
	}
	rm.Logger.Info("found drainable pods", "numPods", len(drainablePods), "nodeName", nodeName)

	// Convert to pointers
	var pods []*v1.Pod
	for i := range drainablePods {
		pods = append(pods, &drainablePods[i])
	}

	return false, k8s.DrainPods(pods, rm.RawClient, unhealthyAfter)
}
