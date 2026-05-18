package node

import (
	"context"
	"time"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	cyclopscontroller "github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/atlassian-labs/cyclops/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimecontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerName = "node.controller"

	// defaultReconcileConcurrency is intentionally low because the node controller
	// is an eventual-consistency backstop, not a high-throughput controller.
	defaultReconcileConcurrency = 1
	defaultRequeueAfter         = 5 * time.Minute

	cyclopsManagedAnnotation                     = k8s.CyclopsManagedAnnotation
	clusterAutoscalerScaleDownDisabledAnnotation = k8s.ClusterAutoscalerScaleDownDisabledAnnotation
)

var log = logf.Log.WithName(controllerName)

// Options contains configuration for the node controller.
type Options struct {
	ReconcileConcurrency int
	RequeueAfter         time.Duration
}

func (o Options) withDefaults() Options {
	if o.ReconcileConcurrency <= 0 {
		o.ReconcileConcurrency = defaultReconcileConcurrency
	}
	if o.RequeueAfter <= 0 {
		o.RequeueAfter = defaultRequeueAfter
	}
	return o
}

// Reconciler watches Node objects and cleans up stale Cyclops-managed annotations
// left behind when a CycleNodeRequest is deleted before reaching a terminal phase.
type Reconciler struct {
	client    client.Client
	rawClient kubernetes.Interface
	namespace string
	options   Options
}

// NewReconciler returns a new Reconciler for Nodes and registers it with the manager.
// It reuses the manager's shared Node cache (already populated by the CNR transitioner)
// to avoid duplicating API traffic.
func NewReconciler(mgr manager.Manager, namespace string, options Options) (reconcile.Reconciler, error) {
	rawClient := kubernetes.NewForConfigOrDie(mgr.GetConfig())
	options = options.withDefaults()

	reconciler := &Reconciler{
		client:    mgr.GetClient(),
		rawClient: rawClient,
		namespace: namespace,
		options:   options,
	}

	nodeController, err := runtimecontroller.New(
		controllerName,
		mgr,
		runtimecontroller.Options{
			Reconciler:              reconciler,
			MaxConcurrentReconciles: options.ReconcileConcurrency,
		})
	if err != nil {
		log.Error(err, "Unable to create node controller")
		return nil, err
	}

	src := source.Kind(
		mgr.GetCache(),
		&corev1.Node{},
		&handler.TypedEnqueueRequestForObject[*corev1.Node]{},
		cyclopscontroller.NewCyclopsManagedNodePredicate(),
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

	if _, ok := node.Annotations[k8s.CyclopsManagedAnnotation]; !ok {
		return reconcile.Result{}, nil
	}

	nodeLabels := labels.Set(node.Labels)

	// Only act on nodes that are under Cyclops management, i.e. selected by at least one NodeGroup.
	nodegroup, err := r.nodegroupForNode(ctx, nodeLabels)
	if err != nil {
		logger.Error(err, "Failed to check NodeGroup membership")
		return reconcile.Result{}, err
	}
	if nodegroup == "" {
		// Defensive: this path should be unreachable. The Cyclops-managed node
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
		logger.V(1).Info("Node is still involved in an active CycleNodeRequest, requeueing", "requeueAfter", r.options.RequeueAfter)
		metrics.NodeCleanupReconciles.WithLabelValues("active_cnr_skipped").Inc()
		return reconcile.Result{RequeueAfter: r.options.RequeueAfter}, nil
	}

	// No active CNR covers this node — clean up the stale annotations.
	logger.Info("Removing stale scale-down-disabled annotations from node")

	if err := k8s.RemoveScaleDownDisabledAnnotationsFromNode(node.Name, r.rawClient); err != nil {
		logger.Error(err, "Failed to remove stale annotations")
		metrics.NodeCleanupReconciles.WithLabelValues("error").Inc()
		return reconcile.Result{}, err
	}

	logger.Info("Successfully removed stale annotations from node")
	metrics.NodeCleanupAnnotationsRemoved.Inc()
	metrics.NodeCleanupReconciles.WithLabelValues("cleaned").Inc()
	metrics.NodesWithAnnotation.WithLabelValues(nodegroup, node.Name).Set(0)
	return reconcile.Result{}, nil
}

// nodegroupForNode returns the cloud-provider nodegroup name (for metrics) of the first
// NodeGroup whose spec.nodeSelector matches the given node labels, or "" if none match.
func (r *Reconciler) nodegroupForNode(ctx context.Context, nodeLabels labels.Set) (string, error) {
	ngList := &atlassianv1.NodeGroupList{}
	if err := r.client.List(ctx, ngList); err != nil {
		return "", err
	}

	for _, ng := range ngList.Items {
		selector, err := metav1.LabelSelectorAsSelector(&ng.Spec.NodeSelector)
		if err != nil {
			log.Error(err, "Invalid NodeGroup nodeSelector, skipping", "nodeGroup", ng.Name)
			continue
		}
		if selector.Matches(nodeLabels) {
			names := ng.GetNodeGroupNames()
			if len(names) > 0 {
				return names[0], nil
			}
			return ng.Name, nil
		}
	}

	return "", nil
}

// isNodeInActiveCNR returns true if the node's labels match the spec.selector of any
// CycleNodeRequest that has not yet reached a terminal phase (Successful or Failed).
func (r *Reconciler) isNodeInActiveCNR(ctx context.Context, nodeLabels labels.Set) (bool, error) {
	cnrList := &atlassianv1.CycleNodeRequestList{}
	if err := r.client.List(ctx, cnrList, client.InNamespace(r.namespace)); err != nil {
		return false, err
	}

	for _, cnr := range cnrList.Items {
		if cnr.IsTerminal() {
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
