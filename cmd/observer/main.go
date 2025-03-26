package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/atlassian-labs/cyclops/pkg/apis"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider/builder"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/atlassian-labs/cyclops/pkg/observer"
	"github.com/atlassian-labs/cyclops/pkg/observer/cloud"
	k8sobserver "github.com/atlassian-labs/cyclops/pkg/observer/k8s"
)

var (
	// replaced by ldflags at buildtime
	version = "undefined" //nolint:golint,varcheck,deadcode,unused
	klogger = textlogger.NewLogger(&textlogger.Config{})
)

// app type holds options for the application from cobra
type app struct {
	namespaces        *[]string
	cloudProviderName *string
	namespace         *string
	addr              *string
	dryMode           *bool
	runImmediately    *bool
	runOnce           *bool
	checkInterval     *time.Duration
	waitInterval      *time.Duration
	nodeStartupTime   *time.Duration
}

// newApp creates a new app and sets up the cobra flags
func newApp(rootCmd *cobra.Command) *app {
	return &app{
		addr:              rootCmd.PersistentFlags().String("addr", ":8080", "Address to listen on for /metrics"),
		cloudProviderName: rootCmd.PersistentFlags().String("cloud-provider", "aws", "Which cloud provider to use, options: [aws]"),
		namespaces:        rootCmd.PersistentFlags().StringSlice("namespaces", []string{"kube-system"}, "Namespaces to watch for cycle request objects"),
		namespace:         rootCmd.PersistentFlags().String("namespace", "kube-system", "Namespaces to watch and create cnrs"),
		dryMode:           rootCmd.PersistentFlags().Bool("dry", false, "api-server drymode for applying CNRs"),
		waitInterval:      rootCmd.PersistentFlags().Duration("wait-interval", 2*time.Minute, "duration to wait after detecting changes before creating CNR objects. The window for letting changes on nodegroups settle before starting rotation"),
		checkInterval:     rootCmd.PersistentFlags().Duration("check-interval", 5*time.Minute, `duration interval to check for changes. e.g. run the loop every 5 minutes"`),
		nodeStartupTime:   rootCmd.PersistentFlags().Duration("node-startup-time", 2*time.Minute, "duration to wait after a cluster-autoscaler scaleUp event is detected"),
		runImmediately:    rootCmd.PersistentFlags().Bool("now", false, "makes the check loop run straight away on program start rather than wait for the check interval to elapse"),
		runOnce:           rootCmd.PersistentFlags().Bool("once", false, "run the check loop once then exit. also works with --now"),
	}
}

// awaitStopSignal awaits termination signals and shutdown gracefully
func awaitStopSignal(stopChan chan struct{}) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-signalChan

	klog.Infof("Signal received: %v", sig)
	klog.Infoln("Stopping observer gracefully")
	close(stopChan)
}

// run sets ups and begins the controller
func (a *app) run() {
	klog.V(3).Infoln("starting up..")

	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintln("Unable to setup Kubernetes CRD schemes", err))
	}

	k8sClient, crdClient := a.getK8SClient(), a.getCRDClient()
	stopCh := make(chan struct{})

	// setup watchers
	var podCaches []cache.Indexer
	var dsCaches []cache.Indexer
	var crCaches []cache.Indexer
	for _, namespace := range *a.namespaces {
		podCaches = append(podCaches, k8s.StartWatching(k8sClient, namespace, k8s.WatchPods, stopCh))
		dsCaches = append(dsCaches, k8s.StartWatching(k8sClient, namespace, k8s.WatchDaemonSets, stopCh))
		crCaches = append(crCaches, k8s.StartWatching(k8sClient, namespace, k8s.WatchControllerRevisions, stopCh))
	}
	podLister := k8s.NewCachedPodList(podCaches...)
	daemonsetLister := k8s.NewCachedDaemonSetList(dsCaches...)
	crLister := k8s.NewCachedControllerRevisionList(crCaches...)

	nodeCache := k8s.StartWatching(k8sClient, "", k8s.WatchNodes, stopCh)
	nodeLister := k8s.NewCachedNodeList(nodeCache)

	// setup observers
	observers := map[string]observer.Observer{}

	k8sObserver := a.createK8SObserver(nodeLister, podLister, daemonsetLister, crLister)
	observers["k8s"] = k8sObserver

	cloudObserver := a.createCloudObserver(nodeLister)
	observers["cloud"] = cloudObserver

	if *a.runOnce {
		// reduce waiting period when runOnce is enabled
		*a.waitInterval = 5 * time.Second
	}

	options := observer.Options{
		CNRPrefix:       "observer",
		Namespace:       *a.namespace,
		CheckInterval:   *a.checkInterval,
		DryMode:         *a.dryMode,
		RunImmediately:  *a.runImmediately,
		RunOnce:         *a.runOnce,
		WaitInterval:    *a.waitInterval,
		NodeStartupTime: *a.nodeStartupTime,
	}

	go awaitStopSignal(stopCh)
	controller := observer.NewController(crdClient, stopCh, options, nodeLister, observers, *a.addr)
	if *a.runOnce {
		controller.Run()
	} else {
		controller.RunForever()
	}
}

// getCRDClient creates a new controller Client for CRDs
func (a *app) getCRDClient() client.Client {
	config, err := k8s.GetConfig("")
	if err != nil {
		panic(err)
	}

	c, err := client.New(config, client.Options{})
	if err != nil {
		panic(err)
	}
	return c
}

// getK8SClient creates a full k8s client for cached standard objects
func (a *app) getK8SClient() kubernetes.Interface {
	config, err := k8s.GetConfig("")
	if err != nil {
		panic(err)
	}
	return kubernetes.NewForConfigOrDie(config)
}

// createK8SObserver creates a new k8s.Observer
func (a *app) createK8SObserver(nodeLister k8s.NodeLister, podLister k8s.PodLister, dsLister k8s.DaemonSetLister, crLister k8s.ControllerRevisionLister) observer.Observer {
	return k8sobserver.NewObserver(nodeLister, podLister, dsLister, crLister)
}

// createCloudObserver creates a new cloud.Observer with the given cloud provider name
func (a *app) createCloudObserver(nodeLister k8s.NodeLister) observer.Observer {
	// Setup the backend cloud provider
	cloudProvider, err := builder.BuildCloudProvider(*a.cloudProviderName, klogger)
	if err != nil {
		klog.Error(err, "Unable to build cloud provider")
		os.Exit(1)
	}
	return cloud.NewObserver(cloudProvider, nodeLister)
}

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()

	// Only log to stderr - not to file, so we can launch on read-only fs
	_ = flag.Set("logtostderr", "true")
	// log Error/Warning/Info to stderr
	_ = flag.Set("stderrthreshold", "0")

	// setup the app and cobra
	var a *app
	rootCmd := &cobra.Command{
		Use:   "cyclops-observer",
		Short: "detects changes on nodegroups (cloud/k8s) and creates CNRs",
		Long:  "detects changes on nodegroups for both cloud instances out of date with ASGs and OnDelete pods of DaemonSets. Will create CNRs to automatically cycle affected nodes",

		Run: func(*cobra.Command, []string) {
			a.run()
		},
	}
	rootCmd.Flags().AddGoFlagSet(flag.CommandLine)
	a = newApp(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		klog.Errorln("failed to execute command:", err)
	}
}
