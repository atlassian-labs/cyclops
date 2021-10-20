package cyclenodestatus

import (
	"context"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	cyclecontroller "github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/atlassian-labs/cyclops/pkg/controller/cyclenodestatus/transitioner"
	"github.com/atlassian-labs/cyclops/pkg/notifications"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerName       = "cyclenodestatus.controller"
	eventName            = "cyclops"
	reconcileConcurrency = 16
)

var log = logf.Log.WithName(controllerName)

// Reconciler reconciles CycleNodeStatuses. It implements reconcile.Reconciler
type Reconciler struct {
	mgr           manager.Manager
	cloudProvider cloudprovider.CloudProvider
	notifier      notifications.Notifier
	rawClient     kubernetes.Interface
	options       transitioner.Options
}

// NewReconciler returns a new Reconciler for CycleNodeStatuses, which implements reconcile.Reconciler
// The Reconciler is registered as a controller and initialised as part of the creation.
func NewReconciler(
	mgr manager.Manager,
	cloudProvider cloudprovider.CloudProvider,
	notifier notifications.Notifier,
	namespace string,
	options transitioner.Options,
) (reconcile.Reconciler, error) {
	rawClient := kubernetes.NewForConfigOrDie(mgr.GetConfig())

	// Create the reconciler
	reconciler := &Reconciler{
		mgr:           mgr,
		cloudProvider: cloudProvider,
		notifier:      notifier,
		rawClient:     rawClient,
		options:       options,
	}

	// Create the new controller using the reconciler. This registers it with the main event loop.
	cnsController, err := controller.New(
		controllerName,
		mgr,
		controller.Options{
			Reconciler:              reconciler,
			MaxConcurrentReconciles: reconcileConcurrency,
		})
	if err != nil {
		log.Error(err, "Unable to create cycleNodeStatus controller")
		return nil, err
	}

	// Initialise the controller's required watches
	err = cnsController.Watch(
		&source.Kind{Type: &v1.CycleNodeStatus{}},
		&handler.EnqueueRequestForObject{},
		cyclecontroller.NewNamespacePredicate(namespace),
	)
	if err != nil {
		return nil, err
	}

	// Setup an indexer for pod spec.nodeName
	if err := mgr.GetFieldIndexer().IndexField(context.TODO(), &coreV1.Pod{}, "spec.nodeName", func(object client.Object) []string {
		p, ok := object.(*coreV1.Pod)
		if !ok {
			return []string{}
		}
		return []string{p.Spec.NodeName}
	}); err != nil {
		return nil, err
	}
	return reconciler, nil
}

// Reconcile reconciles the incoming request, usually a cycleNodeStatus
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.WithValues("name", request.Name, "namespace", request.Namespace, "controller", controllerName)

	// Fetch the CycleNodeStatus from the API server
	cycleNodeStatus := &v1.CycleNodeStatus{}
	err := r.mgr.GetClient().Get(ctx, request.NamespacedName, cycleNodeStatus)
	if err != nil {
		// Object not found, must have been deleted
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to get cycleNodeStatus")
		return reconcile.Result{}, err
	}

	logger = log.WithValues("name", request.Name, "namespace", request.Namespace, "phase", cycleNodeStatus.Status.Phase)
	rm := cyclecontroller.NewResourceManager(
		r.mgr.GetClient(),
		r.rawClient,
		*http.DefaultClient,
		r.mgr.GetEventRecorderFor(eventName),
		logger,
		r.notifier,
		r.cloudProvider)
	result, err := transitioner.NewCycleNodeStatusTransitioner(cycleNodeStatus, rm, r.options).Run()
	return result, err
}
