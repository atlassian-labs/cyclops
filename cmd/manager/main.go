package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/alecthomas/kingpin/v2"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider/builder"
	cyclopsmanager "github.com/atlassian-labs/cyclops/pkg/manager"
	cnrTransitioner "github.com/atlassian-labs/cyclops/pkg/controller/cyclenoderequest/transitioner"
	cnsTransitioner "github.com/atlassian-labs/cyclops/pkg/controller/cyclenodestatus/transitioner"
	nodecontroller "github.com/atlassian-labs/cyclops/pkg/controller/node"
	"github.com/atlassian-labs/cyclops/pkg/notifications"
	"github.com/atlassian-labs/cyclops/pkg/notifications/notifierbuilder"
	"github.com/operator-framework/operator-lib/leader"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	// replaced by ldflags at buildtime
	version = "undefined" //nolint:golint,varcheck,deadcode,unused

	app = kingpin.New("cyclops", "Kubernetes operator to rotate a group of nodes").DefaultEnvars().Version(version)

	debug = app.Flag("debug", "Run with debug logging").Short('d').Bool()

	cloudProviderName     = app.Flag("cloud-provider", "Which cloud provider to use, options: [aws]").Default("aws").String()
	messagingProviderName = app.Flag("messaging-provider", "Which message provider to use, options: [slack] (Optional)").Default("").String()

	addr      = app.Flag("address", "Address to listen on for /metrics").Default(":8080").String()
	namespace = app.Flag("namespace", "Namespace to watch for cycle request objects").Default("kube-system").String()

	healthCheckTimeout = app.Flag("health-check-timeout", "Timeout on health checks performed").Default("5s").Duration()

	deleteCNR                        = app.Flag("delete-cnr", "Whether or not to automatically delete CNRs").Default("false").Bool()
	deleteCNRExpiry                  = app.Flag("delete-cnr-expiry", "Delete the CNR this long after it was created and is successful").Default("168h").Duration()
	deleteCNRRequeue                 = app.Flag("delete-cnr-requeue", "How often to check if a CNR can be deleted").Default("24h").Duration()
	defaultCNScyclingExpiry          = app.Flag("default-cns-cycling-expiry", "Fail the CNS if it has been cycling for this long").Default("3h").Duration()
	unhealthyPodTerminationThreshold = app.Flag("unhealthy-pod-termination-after", "How long to tolerate an un-evictable yet unhealthy pod before forcefully removing it").Default("5m").Duration()

	cnrScaleUpWait              = app.Flag("cnr-scale-up-wait", "Minimum time to wait after scaling up before checking if replacement nodes are Ready").Default("1m").Duration()
	cnrScaleUpLimit             = app.Flag("cnr-scale-up-limit", "Maximum total time to wait for replacement nodes to come up before failing the CNR").Default("20m").Duration()
	cnrNodeEquilibriumWaitLimit = app.Flag("cnr-node-equilibrium-wait-limit", "Maximum time to wait for the kube-node-set and cloud-provider-instance-set to converge during the Initialised phase").Default("5m").Duration()
	cnrTransitionDuration       = app.Flag("cnr-transition-duration", "RequeueAfter used when moving the CNR between phases").Default("10s").Duration()
	cnrRequeueDuration          = app.Flag("cnr-requeue-duration", "RequeueAfter used while the CNR is waiting on an external condition within a phase").Default("30s").Duration()

	cnsTransitionDuration        = app.Flag("cns-transition-duration", "RequeueAfter used when moving the CNS between phases").Default("10s").Duration()
	cnsWaitingPodsRequeue        = app.Flag("cns-waiting-pods-requeue", "RequeueAfter used while waiting for pods on the cycling node to finish naturally (Method=Wait)").Default("60s").Duration()
	cnsRemovingLabelsPodsRequeue = app.Flag("cns-removing-labels-pods-requeue", "RequeueAfter used while removing labels from pods on the cycling node").Default("1s").Duration()
	cnsDrainingRetryRequeue      = app.Flag("cns-draining-retry-requeue", "RequeueAfter used when the apiserver returns 429 TooManyRequests (PDB-blocked) during drain").Default("15s").Duration()
	cnsDrainingPodsRequeue       = app.Flag("cns-draining-pods-requeue", "RequeueAfter used while waiting for the in-flight drain to finish").Default("30s").Duration()

	nodeControllerReconcileConcurrency = app.Flag("node-controller-reconcile-concurrency", "Maximum number of concurrent node controller reconciles").Default("1").Int()
	nodeControllerRequeueAfter         = app.Flag("node-controller-requeue-after", "How often the node controller rechecks annotated nodes that are still covered by an active CNR").Default("5m").Duration()
)

var log = logf.Log.WithName("cmd")

func main() {
	kingpin.MustParse(app.Parse(os.Args[1:]))
	logger := zap.New(zap.UseDevMode(*debug))
	logf.SetLogger(logger)
	printVersion()

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "Unable to get config")
		os.Exit(1)
	}

	ctx := context.TODO()

	// Become the leader before proceeding
	err = leader.Become(ctx, "cyclops-lock")
	if err != nil {
		log.Error(err, "Unable to become leader")
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: *addr,
		},
	})
	if err != nil {
		log.Error(err, "Unable to create a new manager")
		os.Exit(1)
	}

	// Setup the cloud provider
	// Uses AWS SDK's built-in retry behavior
	cloudProvider, err := builder.BuildCloudProvider(*cloudProviderName, logger)
	if err != nil {
		log.Error(err, "Unable to build cloud provider")
		os.Exit(1)
	}

	var notifier notifications.Notifier

	// Setup the notifier if it is enabled
	if *messagingProviderName != "" {
		notifier, err = notifierbuilder.BuildNotifier(*messagingProviderName)
		if err != nil {
			log.Error(err, "Unable to build notifier")
			os.Exit(1)
		}
	}

	log.Info("Starting the Cmd.")

	if err := cyclopsmanager.Run(signals.SetupSignalHandler(), mgr, cyclopsmanager.Dependencies{
		CloudProvider: cloudProvider,
		Notifier:      notifier,
		Namespace:     *namespace,
		CNROptions: cnrTransitioner.Options{
			DeleteCNR:                *deleteCNR,
			DeleteCNRExpiry:          *deleteCNRExpiry,
			DeleteCNRRequeue:         *deleteCNRRequeue,
			HealthCheckTimeout:       *healthCheckTimeout,
			ScaleUpWait:              *cnrScaleUpWait,
			ScaleUpLimit:             *cnrScaleUpLimit,
			NodeEquilibriumWaitLimit: *cnrNodeEquilibriumWaitLimit,
			TransitionDuration:       *cnrTransitionDuration,
			RequeueDuration:          *cnrRequeueDuration,
		},
		CNSOptions: cnsTransitioner.Options{
			DefaultCNScyclingExpiry:          *defaultCNScyclingExpiry,
			UnhealthyPodTerminationThreshold: *unhealthyPodTerminationThreshold,
			TransitionDuration:               *cnsTransitionDuration,
			WaitingPodsRequeue:               *cnsWaitingPodsRequeue,
			RemovingLabelsPodsRequeue:        *cnsRemovingLabelsPodsRequeue,
			DrainingRetryRequeue:             *cnsDrainingRetryRequeue,
			DrainingPodsRequeue:              *cnsDrainingPodsRequeue,
		},
		NodeOptions: nodecontroller.Options{
			ReconcileConcurrency: *nodeControllerReconcileConcurrency,
			RequeueAfter:         *nodeControllerRequeueAfter,
		},
	}); err != nil {
		log.Error(err, "Manager exited non-zero")
		os.Exit(1)
	}
}

func printVersion() {
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("Cyclops Version: %v", version))
}
