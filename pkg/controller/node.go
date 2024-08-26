package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/atlassian-labs/cyclops/pkg/k8s"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	nodeFinalizerName       = "cyclops.atlassian.com/finalizer"
	nodegroupAnnotationName = "cyclops.atlassian.com/nodegroup"
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

func (rm *ResourceManager) AddNodegroupAnnotationToNode(nodeName, nodegroupName string) error {
	return rm.AddAnnotationToNode(nodeName, nodegroupAnnotationName, nodegroupName)
}

func (rm *ResourceManager) GetNodegroupFromNodeAnnotation(nodeName string) (string, error) {
	// Get the node
	node, err := rm.GetNode(nodeName)
	if err != nil {
		return "", err
	}

	nodegroupName, exists := node.Annotations[nodegroupAnnotationName]

	if !exists {
		return "", fmt.Errorf("node %s does not contain the %s annotation",
			nodeName,
			nodegroupAnnotationName,
		)
	}

	return nodegroupName, nil
}

func (rm *ResourceManager) AddAnnotationToNode(nodeName, annotationName, annotationValue string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Let the caller account for the node not being found
		return k8s.AddAnnotationToNode(nodeName, annotationName, annotationValue, rm.RawClient)
	})
}

func (rm *ResourceManager) AddFinalizerToNode(nodeName string) error {
	return rm.manageFinalizerOnNode(nodeName, k8s.AddFinalizerToNode)
}

func (rm *ResourceManager) RemoveFinalizerFromNode(nodeName string) error {
	return rm.manageFinalizerOnNode(nodeName, k8s.RemoveFinalizerFromNode)
}

func (rm *ResourceManager) NodeContainsNonCyclingFinalizer(nodeName string) (bool, error) {
	node, err := rm.GetNode(nodeName)

	// If the node is not found then skip the finalizer check
	if err != nil && apierrors.IsNotFound(err) {
		rm.Logger.Info("node deleted, skip adding finalizer", "node", nodeName)
		return false, nil
	}

	for _, finalizer := range node.Finalizers {
		if finalizer != nodeFinalizerName {
			return true, nil
		}
	}

	return false, nil
}

func (rm *ResourceManager) manageFinalizerOnNode(nodeName string, fn func(*v1.Node, string, kubernetes.Interface) error) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
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
			rm.Logger.Info("updating finalizer failed, node already deleted", "node", nodeName)
			return nil
		}

		return err
	})
}
