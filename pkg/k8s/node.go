package k8s

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// CyclopsManagedAnnotation marks nodes where Cyclops added the scale-down-disabled annotation.
	CyclopsManagedAnnotation = "cyclops.atlassian.com/annotation-managed"

	// ClusterAutoscalerScaleDownDisabledAnnotation prevents Cluster Autoscaler from scaling down a node.
	ClusterAutoscalerScaleDownDisabledAnnotation = "cluster-autoscaler.kubernetes.io/scale-down-disabled"
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
			Path:  fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(labelName, "/", "~1")),
			Value: labelValue,
		},
	}
	return PatchNode(nodeName, patches, client)
}

// AddAnnotationToNode performs a patch operation on a node to add an annotation to the node
func AddAnnotationToNode(nodeName string, annotationName string, annotationValue string, client kubernetes.Interface) error {
	patches := []Patch{
		{
			Op: "add",
			// json patch spec maps "~1" to "/" as an escape sequence, so we do the translation here...
			Path:  fmt.Sprintf("/metadata/annotations/%s", strings.ReplaceAll(annotationName, "/", "~1")),
			Value: annotationValue,
		},
	}
	return PatchNode(nodeName, patches, client)
}

// RemoveAnnotationFromNode performs a patch operation on a node to remove an annotation from the node
func RemoveAnnotationFromNode(nodeName string, annotationName string, client kubernetes.Interface) error {
	patches := []Patch{
		{
			Op:   "remove",
			Path: fmt.Sprintf("/metadata/annotations/%s", strings.ReplaceAll(annotationName, "/", "~1")),
		},
	}
	return PatchNode(nodeName, patches, client)
}

// RemoveAnnotationsFromNode removes annotations from a node with retry-on-conflict.
// Missing nodes or annotations are treated as success so callers can use this for
// best-effort cleanup of stale node state.
func RemoveAnnotationsFromNode(nodeName string, client kubernetes.Interface, annotationNames ...string) error {
	for _, annotationName := range annotationNames {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			node, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if node.Annotations == nil {
				return nil
			}
			if _, exists := node.Annotations[annotationName]; !exists {
				return nil
			}

			return RemoveAnnotationFromNode(nodeName, annotationName, client)
		}); err != nil {
			return err
		}
	}

	return nil
}

// RemoveScaleDownDisabledAnnotationsFromNode removes the scale-down-disabled annotation
// and the Cyclops marker annotation from a node.
func RemoveScaleDownDisabledAnnotationsFromNode(nodeName string, client kubernetes.Interface) error {
	return RemoveAnnotationsFromNode(
		nodeName,
		client,
		ClusterAutoscalerScaleDownDisabledAnnotation,
		CyclopsManagedAnnotation,
	)
}

// RemoveLabelFromNode performs a patch operation on a node to remove a label from the node
func RemoveLabelFromNode(nodeName string, labelName string, client kubernetes.Interface) error {
	patches := []Patch{
		{
			Op:   "remove",
			Path: fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(labelName, "/", "~1")),
		},
	}
	return PatchNode(nodeName, patches, client)
}

// AddFinalizerToNode updates a node to add a finalizer to it
func AddFinalizerToNode(node *v1.Node, finalizerName string, client kubernetes.Interface) error {
	controllerutil.AddFinalizer(node, finalizerName)
	_, err := client.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	return err
}

// RemoveFinalizerFromNode updates a node to remove a finalizer from it
func RemoveFinalizerFromNode(node *v1.Node, finalizerName string, client kubernetes.Interface) error {
	controllerutil.RemoveFinalizer(node, finalizerName)
	_, err := client.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	return err
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

// NodeExists checks if a node exists
func NodeExists(name string, client kubernetes.Interface) (bool, error) {
	_, err := client.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}
