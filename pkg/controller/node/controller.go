package node

import (
	"context"
	"time"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/atlassian-labs/cyclops/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerName       = "node.controller"
	reconcileConcurrency = 1
	requeueAfter         = 5 * time.Minute

	// cyclopsManagedAnnotation marks nodes where Cyclops added the scale-down-disabled annotation.
	cyclopsManagedAnnotation = "cyclops.atlassian.com/annotation-managed"

	// clusterAutoscalerScaleDownDisabledAnnotation is the annotation key used to prevent
	// Cluster Autoscaler from scaling down a node.
	clusterAutoscalerScaleDownDisabledAnnotation = "cluster-autoscaler.kubernetes.io/scale-down-disabled"
)

var log = logf.Log.WithName(controllerName)

// Reconciler watches Node objects and cleans up stale Cyclops-managed annotations
// left behind when a CycleNodeRequest is deleted before reaching a terminal phase.
type Reconciler struct {
	mgr       manager.Manager
	client    client.Client
	rawClient kubernetes.Interface
	namespace string
}

// NewReconciler returns a new Reconciler for Nodes and registers it with the manager.
// It reuses the manager's shared Node cache (already populated by the CNR transitioner)
// to avoid duplicating API traffic.
func NewReconciler(mgr manager.Manager, namespace string) (reconcile.Reconciler, error) {
	rawClient := kubernetes.NewForConfigOrDie(mgr.GetConfig())

	reconciler := &Reconciler{
		mgr:       mgr,
		client:    mgr.GetClient(),
		rawClient: rawClient,
		namespace: namespace,
	}

	nodeController, err := controller.New(
		controllerName,
		mgr,
		controller.Options{
			Reconciler:              reconciler,
			MaxConcurrentReconciles: reconcileConcurrency,
		})
	if err != nil {
		log.Error(err, "Unable to create node controller")
		return nil, err
	}

	src := source.Kind(
		mgr.GetCache(),
		&corev1.Node{},
		&handler.TypedEnqueueRequestForObject[*corev1.Node]{},
		hasCyclopsManagedAnnotation{},
	)
	if err := nodeController.Watch(src); err != nil {
		log.Error(err, "Unable to watch Node objects")
		return nil, err
	}

	return reconciler, nil
}

// Reconcile checks whether a Node with the cyclopsManagedAnnotation is still involved
// in an active CycleNodeRequest. If it is not, both the scale-down-disabled annotation
// and the Cyclops marker annotation are removed. Nodes not selected by any NodeGroup
// are skipped as they are not under Cyclops management.
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.WithValues("node", request.Name, "controller", controllerName)

	node := &corev1.Node{}
	if err := r.client.Get(ctx, request.NamespacedName, node); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to get node")
		return reconcile.Result{}, err
	}

	if !hasAnnotation(node.Annotations, cyclopsManagedAnnotation) || !hasAnnotation(node.Annotations, clusterAutoscalerScaleDownDisabledAnnotation) {
		return reconcile.Result{}, nil
	}

	nodeLabels := labels.Set(node.Labels)

	// Only act on nodes that are under Cyclops management, i.e. selected by at least one NodeGroup.
	managed, err := r.isNodeManagedByNodeGroup(ctx, nodeLabels)
	if err != nil {
		logger.Error(err, "Failed to check NodeGroup membership")
		return reconcile.Result{}, err
	}
	if !managed {
		// Defensive: this path should be unreachable. The hasCyclopsManagedAnnotation
		// predicate ensures we only reconcile nodes that Cyclops annotated during
		// cycling, and Cyclops only annotates nodes selected by a NodeGroup.
		// The only way to reach here is if the NodeGroup was deleted after the
		// annotation was applied, or if someone manually added the annotation.
		// Expect this metric to stay at 0.
		logger.Info("Node is not selected by any NodeGroup, skipping")
		metrics.NodeCleanupReconciles.WithLabelValues("no_nodegroup_skipped").Inc()
		return reconcile.Result{}, nil
	}

	// Check whether any active (non-terminal) CNR still covers this node.
	active, err := r.isNodeInActiveCNR(ctx, nodeLabels)
	if err != nil {
		logger.Error(err, "Failed to check active CycleNodeRequests")
		metrics.NodeCleanupReconciles.WithLabelValues("error").Inc()
		return reconcile.Result{}, err
	}
	if active {
		logger.Info("Node is still involved in an active CycleNodeRequest, requeueing", "requeueAfter", requeueAfter)
		metrics.NodeCleanupReconciles.WithLabelValues("active_cnr_skipped").Inc()
		return reconcile.Result{RequeueAfter: requeueAfter}, nil
	}

	// No active CNR covers this node — clean up the stale annotations.
	logger.Info("Removing stale scale-down-disabled annotations from node")

	if err := removeAnnotationWithRetry(node.Name, clusterAutoscalerScaleDownDisabledAnnotation, r.rawClient); err != nil {
		logger.Error(err, "Failed to remove cluster-autoscaler annotation")
		metrics.NodeCleanupReconciles.WithLabelValues("error").Inc()
		return reconcile.Result{}, err
	}

	if err := removeAnnotationWithRetry(node.Name, cyclopsManagedAnnotation, r.rawClient); err != nil {
		logger.Error(err, "Failed to remove Cyclops managed annotation")
		metrics.NodeCleanupReconciles.WithLabelValues("error").Inc()
		return reconcile.Result{}, err
	}

	logger.Info("Successfully removed stale annotations from node")
	metrics.NodeCleanupAnnotationsRemoved.Inc()
	metrics.NodeCleanupReconciles.WithLabelValues("cleaned").Inc()
	return reconcile.Result{}, nil
}

// isNodeManagedByNodeGroup returns true if the node's labels match at least one
// NodeGroup's spec.nodeSelector. NodeGroups are cluster-scoped.
func (r *Reconciler) isNodeManagedByNodeGroup(ctx context.Context, nodeLabels labels.Set) (bool, error) {
	ngList := &atlassianv1.NodeGroupList{}
	if err := r.client.List(ctx, ngList); err != nil {
		return false, err
	}

	for _, ng := range ngList.Items {
		selector, err := metav1.LabelSelectorAsSelector(&ng.Spec.NodeSelector)
		if err != nil {
			log.Error(err, "Invalid NodeGroup nodeSelector, skipping", "nodeGroup", ng.Name)
			continue
		}
		if selector.Matches(nodeLabels) {
			return true, nil
		}
	}

	return false, nil
}

// isNodeInActiveCNR returns true if the node's labels match the spec.selector of any
// CycleNodeRequest that has not yet reached a terminal phase (Successful or Failed).
func (r *Reconciler) isNodeInActiveCNR(ctx context.Context, nodeLabels labels.Set) (bool, error) {
	cnrList := &atlassianv1.CycleNodeRequestList{}
	if err := r.client.List(ctx, cnrList, client.InNamespace(r.namespace)); err != nil {
		return false, err
	}

	for _, cnr := range cnrList.Items {
		if isTerminalPhase(cnr.Status.Phase) {
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(&cnr.Spec.Selector)
		if err != nil {
			log.Error(err, "Invalid CNR selector, skipping", "cnr", cnr.Name)
			continue
		}
		if selector.Matches(nodeLabels) {
			return true, nil
		}
	}

	return false, nil
}

// isTerminalPhase returns true for phases where the CNR lifecycle has ended and
// annotation cleanup has already been handled (or is no longer expected).
func isTerminalPhase(phase atlassianv1.CycleNodeRequestPhase) bool {
	return phase == atlassianv1.CycleNodeRequestSuccessful || phase == atlassianv1.CycleNodeRequestFailed
}

// removeAnnotationWithRetry removes an annotation from a node, retrying on conflict.
// A not-found error (node deleted or annotation already absent) is treated as success.
func removeAnnotationWithRetry(nodeName, annotation string, rawClient kubernetes.Interface) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err := k8s.RemoveAnnotationFromNode(nodeName, annotation, rawClient)
		if err != nil && errors.IsNotFound(err) {
			return nil
		}
		return err
	})
}

func hasAnnotation(annotations map[string]string, key string) bool {
	_, ok := annotations[key]
	return ok
}

// hasCyclopsManagedAnnotation is a controller-runtime predicate that passes only
// Node events where the node carries the cyclopsManagedAnnotation. This avoids
// reconciling every node in the cluster.
type hasCyclopsManagedAnnotation struct{}

func (hasCyclopsManagedAnnotation) Create(e event.TypedCreateEvent[*corev1.Node]) bool {
	return hasAnnotation(e.Object.Annotations, cyclopsManagedAnnotation)
}

func (hasCyclopsManagedAnnotation) Update(e event.TypedUpdateEvent[*corev1.Node]) bool {
	return hasAnnotation(e.ObjectNew.Annotations, cyclopsManagedAnnotation)
}

func (hasCyclopsManagedAnnotation) Delete(event.TypedDeleteEvent[*corev1.Node]) bool {
	return false
}

func (hasCyclopsManagedAnnotation) Generic(e event.TypedGenericEvent[*corev1.Node]) bool {
	return hasAnnotation(e.Object.Annotations, cyclopsManagedAnnotation)
}

var _ predicate.TypedPredicate[*corev1.Node] = hasCyclopsManagedAnnotation{}
