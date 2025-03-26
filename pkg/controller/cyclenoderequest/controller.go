package cyclenoderequest

import (
	"context"
	"fmt"
	"net/http"
	"os"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	cyclecontroller "github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/atlassian-labs/cyclops/pkg/controller/cyclenoderequest/transitioner"
	"github.com/atlassian-labs/cyclops/pkg/notifications"
	"github.com/hashicorp/go-version"
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
	controllerName             = "cyclenoderequest.controller"
	eventName                  = "cyclops"
	reconcileConcurrency       = 1
	clusterNameEnv             = "CLUSTER_NAME"
	ClientAPIVersionAnnotation = "client.api.version"
)

var (
	log = logf.Log.WithName(controllerName)
	// replaced by ldflags at buildtime
	apiVersion = "undefined" //nolint:golint,varcheck,deadcode,unused

)

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
	src := source.Kind(mgr.GetCache(), &v1.CycleNodeRequest{}, &handler.TypedEnqueueRequestForObject[*v1.CycleNodeRequest]{}, cyclecontroller.NewNamespacePredicate[*v1.CycleNodeRequest](namespace))
	err = cnrController.Watch(src)
	if err != nil {
		log.Error(err, "Unable to watch CycleNodeRequest objects")
		return nil, err
	}
	return reconciler, nil
}

// Validates the tls configuration for a pre-termination check or healthcheck and
// returns an error if these are misconfigured
// There are 3 valid modes:
// No fields:   no TLS
// CA only:     TLS
// All fields:  mTLS
func tlsCertsValid(tlsConfig v1.TLSConfig) error {
	// Check that either both the the tls cert and private key are added or missing
	// If they are both added, cyclops will forward them when making requests for mTLS
	// If they are both missing, they will not be added. No mTLS.
	rootCA := os.Getenv(tlsConfig.RootCA)
	certificate := os.Getenv(tlsConfig.Certificate)
	key := os.Getenv(tlsConfig.Key)

	if (certificate == "" && key != "") || (certificate != "" && key == "") {
		return fmt.Errorf("cert or key missing, ensure neither are missing for mTLS")
	}

	// Check that if the certificate and key and both present, the root CA must also
	// be present. At this point the certificate and key are either both present or
	// missing so checking one or the other is the same thing.
	if rootCA == "" && certificate != "" {
		return fmt.Errorf("the cert and key are both added but the root CA is missing, mTLS will fail")
	}

	return nil
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

	cnrClientAPIVersionAnnotation := cycleNodeRequest.Annotations[ClientAPIVersionAnnotation]
	if cnrClientAPIVersionAnnotation != "" {
		controllerAPIVersion, err := version.NewVersion(apiVersion)
		if err != nil {
			return reconcile.Result{}, err
		}
		clientAPIVersion, err := version.NewVersion(cnrClientAPIVersionAnnotation)
		if err != nil {
			return reconcile.Result{}, err
		}
		if clientAPIVersion.LessThan(controllerAPIVersion) {
			cycleNodeRequest.Status.Phase = v1.CycleNodeRequestFailed
			cycleNodeRequest.Status.Message = "Client API version " + cnrClientAPIVersionAnnotation + " does not match controller API version " + apiVersion
		}
	}

	if r.notifier != nil {
		// Initialise the map which will hold the unique selected nodes
		if cycleNodeRequest.Status.SelectedNodes == nil {
			cycleNodeRequest.Status.SelectedNodes = map[string]bool{}
		}

		// Extract the cluster name and add it to the cycleNodeRequest
		if cycleNodeRequest.ClusterName == "" {
			clusterName, ok := os.LookupEnv(clusterNameEnv)
			if !ok {
				return reconcile.Result{}, fmt.Errorf("missing cluster name")
			}

			cycleNodeRequest.ClusterName = clusterName
		}
	}

	httpClient := &http.Client{
		Timeout: r.options.HealthCheckTimeout,
	}

	for i, healthCheck := range cycleNodeRequest.Spec.HealthChecks {
		if len(healthCheck.ValidStatusCodes) == 0 {
			cycleNodeRequest.Spec.HealthChecks[i].ValidStatusCodes = []uint{200}
		}

		// Validate the tls certs before starting to cycle. The certs are optional.
		if err := tlsCertsValid(healthCheck.TLSConfig); err != nil {
			return reconcile.Result{}, err
		}
	}

	if len(cycleNodeRequest.Spec.HealthChecks) > 0 && cycleNodeRequest.Status.HealthChecks == nil {
		cycleNodeRequest.Status.HealthChecks = make(map[string]v1.HealthCheckStatus)
	}

	for i, preTerminationCheck := range cycleNodeRequest.Spec.PreTerminationChecks {
		if len(preTerminationCheck.ValidStatusCodes) == 0 {
			cycleNodeRequest.Spec.PreTerminationChecks[i].ValidStatusCodes = []uint{200}
		}

		if len(preTerminationCheck.HealthCheck.ValidStatusCodes) == 0 {
			cycleNodeRequest.Spec.PreTerminationChecks[i].HealthCheck.ValidStatusCodes = []uint{200}
		}

		// Validate the tls certs before starting to cycle. The certs are optional.
		if err := tlsCertsValid(preTerminationCheck.TLSConfig); err != nil {
			return reconcile.Result{}, err
		}

		// Validate the tls certs before starting to cycle. The certs are optional.
		if err := tlsCertsValid(preTerminationCheck.HealthCheck.TLSConfig); err != nil {
			return reconcile.Result{}, err
		}
	}

	if len(cycleNodeRequest.Spec.PreTerminationChecks) > 0 && cycleNodeRequest.Status.PreTerminationChecks == nil {
		cycleNodeRequest.Status.PreTerminationChecks = make(map[string]v1.PreTerminationCheckStatusList)
	}

	logger = log.WithValues("name", request.Name, "namespace", request.Namespace, "phase", cycleNodeRequest.Status.Phase)
	rm := cyclecontroller.NewResourceManager(
		r.mgr.GetClient(),
		r.rawClient,
		httpClient,
		r.mgr.GetEventRecorderFor(eventName),
		logger,
		r.notifier,
		r.cloudProvider)
	result, err := transitioner.NewCycleNodeRequestTransitioner(cycleNodeRequest, rm, r.options).Run()
	return result, err
}
