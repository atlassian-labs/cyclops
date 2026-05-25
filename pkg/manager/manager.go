// Package manager provides the Run helper that wires up all cyclops
// controllers and starts the controller-runtime manager.
//
// Both cmd/manager/main.go and the integration test suite use this package
// so the production wiring and the test wiring stay in sync.
package manager

import (
	"context"
	"fmt"

	"github.com/atlassian-labs/cyclops/pkg/apis"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/atlassian-labs/cyclops/pkg/controller/cyclenoderequest"
	cnrTransitioner "github.com/atlassian-labs/cyclops/pkg/controller/cyclenoderequest/transitioner"
	"github.com/atlassian-labs/cyclops/pkg/controller/cyclenodestatus"
	cnsTransitioner "github.com/atlassian-labs/cyclops/pkg/controller/cyclenodestatus/transitioner"
	nodecontroller "github.com/atlassian-labs/cyclops/pkg/controller/node"
	"github.com/atlassian-labs/cyclops/pkg/metrics"
	"github.com/atlassian-labs/cyclops/pkg/notifications"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Dependencies holds the external collaborators required to run cyclops.
// The zero value is valid for optional fields (Notifier may be nil).
type Dependencies struct {
	// CloudProvider is used by the CNR and CNS reconcilers to interact with
	// the cloud provider (e.g. AWS). Required.
	CloudProvider cloudprovider.CloudProvider

	// Notifier is used to push progress notifications (e.g. Slack). Optional.
	Notifier notifications.Notifier

	// Namespace is the Kubernetes namespace where CycleNodeRequests and
	// CycleNodeStatuses are watched.
	Namespace string

	// CNROptions configures the CycleNodeRequest transitioner.
	CNROptions cnrTransitioner.Options

	// CNSOptions configures the CycleNodeStatus transitioner.
	CNSOptions cnsTransitioner.Options

	// NodeOptions configures the node reconciler.
	NodeOptions nodecontroller.Options
}

// Run registers the cyclops scheme, all three controllers, and metrics on
// the given manager, then starts the manager. It blocks until ctx is
// cancelled or the manager exits.
//
// This function is the single source of truth for controller wiring. Both
// cmd/manager/main.go and the integration test suite call it so that the
// production wiring and the test wiring stay in sync.
func Run(ctx context.Context, mgr manager.Manager, deps Dependencies) error {
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		return fmt.Errorf("adding cyclops scheme: %w", err)
	}

	metrics.Register(mgr.GetClient(), ctrl.Log, deps.Namespace)

	if _, err := cyclenoderequest.NewReconciler(mgr, deps.CloudProvider, deps.Notifier, deps.Namespace, deps.CNROptions); err != nil {
		return fmt.Errorf("registering CycleNodeRequest reconciler: %w", err)
	}
	if _, err := cyclenodestatus.NewReconciler(mgr, deps.CloudProvider, deps.Notifier, deps.Namespace, deps.CNSOptions); err != nil {
		return fmt.Errorf("registering CycleNodeStatus reconciler: %w", err)
	}
	if _, err := nodecontroller.NewReconciler(mgr, deps.Namespace, deps.NodeOptions); err != nil {
		return fmt.Errorf("registering Node reconciler: %w", err)
	}

	return mgr.Start(ctx)
}
