package cyclenoderequest

import (
	"context"
	"fmt"
	"os"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	cyclecontroller "github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/atlassian-labs/cyclops/pkg/controller/cyclenoderequest/transitioner"
	"github.com/atlassian-labs/cyclops/pkg/notifications"
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
	controllerName       = "cyclenoderequest.controller"
	eventName            = "cyclops"
	reconcileConcurrency = 1
	clusterNameEnv       = "CLUSTER_NAME"
)

var log = logf.Log.WithName(controllerName)

// Reconciler reconciles CycleNodeRequests. It implements reconcile.Reconciler
type Reconciler struct {
	mgr           manager.Manager
	cloudProvider cloudprovider.CloudProvider
	notifier      notifications.Notifier
	rawClient     kubernetes.Interface
	options       transitioner.Options
}

// NewReconciler returns a new Reconciler for CycleNodeRequests, which implements reconcile.Reconciler
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
	cnrController, err := controller.New(
		controllerName,
		mgr,
		controller.Options{
			Reconciler:              reconciler,
			MaxConcurrentReconciles: reconcileConcurrency,
		})
	if err != nil {
		log.Error(err, "Unable to create cycleNodeRequest controller")
		return nil, err
	}

	// Initialise the controller's required watches
	err = cnrController.Watch(
		&source.Kind{Type: &v1.CycleNodeRequest{}},
		&handler.EnqueueRequestForObject{},
		cyclecontroller.NewNamespacePredicate(namespace),
	)
	if err != nil {
		log.Error(err, "Unable to watch CycleNodeRequest objects")
		return nil, err
	}
	return reconciler, nil
}

// Reconcile reconciles the incoming request, usually a cycleNodeRequest
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	logger := log.WithValues("name", request.Name, "namespace", request.Namespace, "controller", controllerName)

	// Fetch the cycle node request from the API server
	cycleNodeRequest := &v1.CycleNodeRequest{}
	err := r.mgr.GetClient().Get(ctx, request.NamespacedName, cycleNodeRequest)
	if err != nil {
		// Object not found, must have been deleted
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to get cycleNodeRequest")
		return reconcile.Result{}, err
	}

	if r.notifier != nil {
		// Initialise the map which will hold the unique selected nodes
		if cycleNodeRequest.Status.SelectedNodes == nil {
			cycleNodeRequest.Status.SelectedNodes = map[string]bool{}
		}

		// Extract the cluster name and add it to the cycleNodeRequest
		if cycleNodeRequest.ClusterName == "" {
			clusterName := os.Getenv(clusterNameEnv)

			if clusterName == "" {
				return reconcile.Result{}, fmt.Errorf("Missing cluster name")
			}

			cycleNodeRequest.ClusterName = clusterName
		}
	}

	logger = log.WithValues("name", request.Name, "namespace", request.Namespace, "phase", cycleNodeRequest.Status.Phase)
	rm := cyclecontroller.NewResourceManager(
		r.mgr.GetClient(),
		r.rawClient,
		r.mgr.GetEventRecorderFor(eventName),
		logger,
		r.notifier,
		r.cloudProvider)
	result, err := transitioner.NewCycleNodeRequestTransitioner(cycleNodeRequest, rm, r.options).Run()
	return result, err
}
