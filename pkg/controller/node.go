package controller

import (
	"context"
	"time"

	"github.com/atlassian-labs/cyclops/pkg/k8s"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	nodeFinalizerName = "cyclops.atlassian.com/finalizer"
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

	// If the node is not found then skip trying to delete it
	if err != nil && apierrors.IsNotFound(err) {
		rm.Logger.Info("node already deleted, skip deleting", "node", name)
		return nil
	}

	// Account for any other possible errors
	if err != nil {
		return err
	}

	// The node exists as of the previous step, try deleting it now
	err = rm.Client.Delete(context.TODO(), node)

	// Account for possible race conditions with other controllers managing
	// nodes in the cluster
	if apierrors.IsNotFound(err) {
		rm.Logger.Info("node deletion attemp failed, node already deleted", "node", name)
		return nil
	}

	return err
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

func (rm *ResourceManager) AddFinalizerToNode(nodeName string) error {
	return rm.manageFinalizerOnNode(nodeName, k8s.AddFinalizerToNode)
}

func (rm *ResourceManager) RemoveFinalizerFromNode(nodeName string) error {
	return rm.manageFinalizerOnNode(nodeName, k8s.RemoveFinalizerFromNode)
}

func (rm *ResourceManager) manageFinalizerOnNode(nodeName string, fn func(*v1.Node, string, kubernetes.Interface) error) error {
	// Get the node
	node, err := rm.GetNode(nodeName)

	// If the node is not found then skip the finalizer operation
	if err != nil && apierrors.IsNotFound(err) {
		rm.Logger.Info("node deleted, skip adding finalizer", "node", nodeName)
		return nil
	}

	// Account for any other possible errors
	if err != nil {
		return err
	}

	// The node exists as of the previous step, try managing the finalizer for
	// it now
	err = fn(node, nodeFinalizerName, rm.RawClient)

	// Account for possible race conditions with other controllers managing
	// nodes in the cluster
	if apierrors.IsNotFound(err) {
		rm.Logger.Info("adding finalizer failed, node already deleted", "node", nodeName)
		return nil
	}

	return err
}
