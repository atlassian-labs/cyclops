package observer

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/generation"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

var apiVersion = "undefined" //nolint:golint,varcheck,deadcode,unused

// controller implements the Controller interface for running observers to detect changes and creating CNRs
type controller struct {
	client     client.Client
	stopCh     <-chan struct{}
	nodeLister k8s.NodeLister
	observers  map[string]Observer

	optimisedOrder []timedKey

	*metrics
	Options
}

// timedKey represents a key (observer key) to a duration (runTime)
// used for optimising the order of observers
type timedKey struct {
	duration time.Duration
	key      string
}

// runMetricsHandler creates the metrics struct for the controller and starts the handler and server
func runMetricsHandler(stopCh <-chan struct{}, addr string) *metrics {
	// setup metrics and http handler
	metrics := newMetrics()
	collectMetricsStruct(metrics)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := http.Server{Addr: addr, Handler: mux}

	// listen and serve on new thread until closed
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Fatalln("metrics server failed:", err)
		}
	}()

	// wait on stopCh to send the shutdown signal to the server
	go func() {
		<-stopCh
		if err := server.Shutdown(context.Background()); err != nil {
			klog.Fatalln("failed to shutdown metrics server:", err)
		}
	}()

	return metrics
}

// NewController creates an implementation of a controller for observing changes and returns the public Controller interface
func NewController(client client.Client, stopCh <-chan struct{}, options Options, nodeLister k8s.NodeLister, observers map[string]Observer, metricsAddr string) Controller {
	// the initial order doesn't matter, just setup the keys
	var initialOrder []timedKey
	for k := range observers {
		initialOrder = append(initialOrder, timedKey{
			key: k,
		})
	}

	return &controller{
		client:         client,
		observers:      observers,
		nodeLister:     nodeLister,
		optimisedOrder: initialOrder,
		stopCh:         stopCh,

		metrics: runMetricsHandler(stopCh, metricsAddr),
		Options: options,
	}
}

// unionNodes returns the union (deduped) list of nodes between the 2 node lists
func unionNodes(aa []*corev1.Node, bb []*corev1.Node) []*corev1.Node {
	unionMap := make(map[string]*corev1.Node, len(aa))
	for i, a := range aa {
		unionMap[a.Name] = aa[i]
	}
	for j, b := range bb {
		unionMap[b.Name] = bb[j]
	}

	unionArray := make([]*corev1.Node, 0, len(unionMap))
	for k := range unionMap {
		unionArray = append(unionArray, unionMap[k])
	}
	return unionArray
}

// observeChanges iterates all observers in the controller and returns a combined list of changed node groups
// nodegroups that have changes in one observer will be skipped by the subsequent observers in order to reduce unnecessary api calls
// the order of observers is optimised each run by their runtime. This makes heavier unnecessary api calls less likely
func (c *controller) observeChanges(validNodeGroups v1.NodeGroupList) []*ListedNodeGroups {
	if len(validNodeGroups.Items) == 0 {
		klog.V(2).Infoln("no valid no groups to check for changes")
	}

	// record latest run times to optimise
	var runTimes []timedKey
	// poll observers to get changed status and collect on nodegroup so we don't have duplicates across observers
	changedMap := make(map[string]*ListedNodeGroups)
	klog.V(3).Infoln("running in optimised order:", c.optimisedOrder)
	for _, key := range c.optimisedOrder {
		obsName := key.key
		obs, ok := c.observers[obsName]
		if !ok {
			klog.Fatalln("failed to get observer from optimised ordering list. Make sure to use NewController")
		}

		klog.V(3).Infof("about to run observer %q", obsName)

		// filter out nodegroups we already know are dirty
		var cleanNodeGroups v1.NodeGroupList
		for i, nodeGroup := range validNodeGroups.Items {
			if _, ok := changedMap[nodeGroup.Name]; ok {
				klog.V(2).Infof("nodegroup %q already known out of date: skipping", nodeGroup.Name)
				continue
			}
			cleanNodeGroups.Items = append(cleanNodeGroups.Items, validNodeGroups.Items[i])
		}

		start := time.Now()
		changedNodeGroups := obs.Changed(&cleanNodeGroups)
		end := time.Now()

		// log the runTime for optimisation
		klog.V(2).Infof("%s: %d nodegroups out of date", obsName, len(changedNodeGroups))
		duration := end.Sub(start)
		runTimes = append(runTimes, timedKey{
			duration: duration,
			key:      obsName,
		})
		klog.V(3).Infof("observer %q time taken: %v", obsName, duration)
		c.ObserverRunTimes.WithLabelValues(obsName).Set(duration.Seconds())

		// collect out of date nodes into the overall map of out of date nodes
		for i, nodeGroup := range changedNodeGroups {
			c.NodeGroupsOutOfDate.WithLabelValues(obsName).Inc()

			if existing, ok := changedMap[nodeGroup.NodeGroup.Name]; ok {
				existing.List = unionNodes(existing.List, nodeGroup.List)
				continue
			}
			changedMap[nodeGroup.NodeGroup.Name] = changedNodeGroups[i]
		}
	}

	// sort new runtimes and update the controller for next run
	sort.Slice(runTimes, func(i, j int) bool {
		return runTimes[i].duration < runTimes[j].duration
	})
	c.optimisedOrder = runTimes

	if len(changedMap) == 0 {
		return nil
	}

	// convert map back into list
	changedList := make([]*ListedNodeGroups, 0, len(changedMap))
	for name := range changedMap {
		changedList = append(changedList, changedMap[name])
	}
	return changedList
}

// validNodeGroups lists all the nodegroups in the cluster and filters out non valid ones
// see generation.ValidateNodeGroup for validation criteria
func (c *controller) validNodeGroups() v1.NodeGroupList {
	// List and validate nodegroups
	options := &client.ListOptions{}
	allNodeGroups, err := generation.ListNodeGroups(c.client, options)
	if err != nil {
		klog.Fatalln("could not list nodegroups", err)
	}

	var validNodeGroups v1.NodeGroupList
	for i, nodeGroup := range allNodeGroups.Items {
		if valid, reason := generation.ValidateNodeGroup(c.nodeLister, nodeGroup); !valid {
			klog.Warningln("skipping nodegroup", nodeGroup.Name, "because", reason)
			continue
		}
		validNodeGroups.Items = append(validNodeGroups.Items, allNodeGroups.Items[i])
	}
	return validNodeGroups
}

// inProgressCNRs lists the CNRs that are not in the phase CycleNodeRequestSuccessful
// only successful CNRs are considered done. Failed is not done
func (c *controller) inProgressCNRs() v1.CycleNodeRequestList {
	// List and check cnrs still in progress
	options := &client.ListOptions{Namespace: c.Namespace}
	allCNRs, err := generation.ListCNRs(c.client, options)
	if err != nil {
		klog.Fatalln("could not list cnrs", err)
	}

	var inProgessCNRs v1.CycleNodeRequestList
	for i, cnr := range allCNRs.Items {
		if cnr.Status.Phase != v1.CycleNodeRequestSuccessful {
			inProgessCNRs.Items = append(inProgessCNRs.Items, allCNRs.Items[i])
		}
	}

	return inProgessCNRs
}

// dropInProgressNodeGroups matches nodeGroups to CNRs and filters out any that match
func (c *controller) dropInProgressNodeGroups(nodeGroups v1.NodeGroupList, cnrs v1.CycleNodeRequestList) v1.NodeGroupList {
	// Filter out nodegroups that aren't currently in progress with a cnr. Count
	// failed CNRs only if they don't outnumber the max threshold defined for
	// the nodegroup
	var restingNodeGroups v1.NodeGroupList

	for i, nodeGroup := range nodeGroups.Items {
		var dropNodeGroup bool
		var failedCNRsFound uint

		for _, cnr := range cnrs.Items {
			// CNR doesn't match nodegroup, skip it
			if !cnr.IsFromNodeGroup(nodeGroup) {
				continue
			}

			// Count the Failed CNRs separately, they need to be counted before
			// they can be considered to drop the nodegroup
			if cnr.Status.Phase == v1.CycleNodeRequestFailed {
				failedCNRsFound++
			} else {
				dropNodeGroup = true
			}

			// If the number of Failed CNRs exceeds the threshold in the
			// nodegroup then drop it
			if failedCNRsFound > nodeGroup.Spec.MaxFailedCycleNodeRequests {
				dropNodeGroup = true
			}
		}

		if dropNodeGroup {
			if failedCNRsFound > nodeGroup.Spec.MaxFailedCycleNodeRequests {
				klog.Warningf("nodegroup %q has too many failed CNRs.. skipping this nodegroup", nodeGroup.Name)
			} else {
				klog.Warningf("nodegroup %q has an in progress CNR.. skipping this nodegroup", nodeGroup.Name)
			}

			c.NodeGroupsLocked.WithLabelValues(nodeGroup.Name).Inc()
			continue
		}

		restingNodeGroups.Items = append(restingNodeGroups.Items, nodeGroups.Items[i])
	}

	return restingNodeGroups
}

// get the cluster-autoscaler last scaleUp activity time
func stringToTime(s string) (time.Time, error) {
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}

// query cluster-autoscaler metrics to figure out if it's safe to start a new CNR
func (c *controller) safeToStartCycle() bool {
	client, err := api.NewClient(api.Config{
		Address: c.PrometheusAddress,
	})
	if err != nil {
		// Prometheus might not be installed in the cluster. return true if it can't connect
		klog.Errorln("Error creating client:", err)
		return true
	}

	v1api := promv1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// scaleDown metric is updated every cycle cluster-autoscaler is checking if the cluster should scaleDown
	// scaleDown does not get checked and therefore not updated when the cluster is scaling up since no check for scaleDown is needed
	result, warnings, err := v1api.Query(ctx, "cluster_autoscaler_last_activity{activity='scaleDown'}", time.Now())
	if err != nil {
		// cluster-autoscaler might not be installed in the cluster. return true if it can't find the metrics of run the query
		klog.Errorln("Error querying Prometheus:", err)
		return true
	}
	if len(warnings) > 0 {
		klog.Errorln("Warnings:", warnings)
	}

	v := result.(model.Vector)
	// cluster-autoscaler should always gives a response if it's active
	if v.Len() == 0 {
		klog.Errorln("Empty response from prometheus")
		return true
	}

	scaleUpTime := v[v.Len()-1].Value.String()
	t, err := stringToTime(scaleUpTime)
	if err != nil {
		klog.Errorln("Error converting the time:", err)
		return false
	}

	// cluster_autoscaler_last_activity values will update every PrometheusScrapeInterval in non-scaling scenario
	lastScaleEvent := time.Since(t)
	if lastScaleEvent > c.PrometheusScrapeInterval {
		klog.Infoln("Scale up event recently happened")
		return false
	}
	klog.V(3).Infoln("No scale up event")

	return true
}

// createCNRs generates and applies CNRs from the changedNodeGroups
func (c *controller) createCNRs(changedNodeGroups []*ListedNodeGroups) {
	klog.V(3).Infoln("applying")
	for _, nodeGroup := range changedNodeGroups {
		nodeNames := make([]string, 0, len(nodeGroup.List))
		for _, node := range nodeGroup.List {
			nodeNames = append(nodeNames, node.Name)
		}
		// generate cnr with prefix and use generate name method
		cnr := generation.GenerateCNR(*nodeGroup.NodeGroup, nodeNames, c.CNRPrefix, c.Namespace)
		generation.UseGenerateNameCNR(&cnr)
		generation.GiveReason(&cnr, nodeGroup.Reason)
		generation.SetAPIVersion(&cnr, apiVersion)

		name := generation.GetName(cnr.ObjectMeta)

		if err := generation.ApplyCNR(c.client, c.DryMode, cnr); err != nil {
			klog.Errorf("failed to apply cnr %q for nodegroup %q: %s", name, nodeGroup.NodeGroup.Name, err)
		} else {
			var drymodeStr string
			if c.DryMode {
				drymodeStr = "[drymode] "
			}
			klog.V(2).Infof("%ssuccessfully applied cnr %q for nodegroup %q", drymodeStr, name, nodeGroup.NodeGroup.Name)
			c.CNRsCreated.WithLabelValues(nodeGroup.NodeGroup.Name).Inc()
		}
	}
}

// nextRunTime returns the next time the controller loop will run from now in UTC
func (c *controller) nextRunTime() time.Time {
	return time.Now().UTC().Add(c.CheckInterval)
}

func (c *controller) checkIfSafeToStartCycle() bool {
	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = 120 * time.Second

	err := backoff.Retry(func() error {
		if !c.safeToStartCycle() {
			klog.Error("Cluster autoscaler scaleUp event in progress. Retry...")
			return errors.New("cluster-autoscaler event in progress")
		}
		return nil
	}, b)

	if err != nil {
		klog.Errorln("there are still cluster-autoscaler scaleUp events")
		return false
	}

	return true
}

// Run runs the controller loops once. detecting lock, changes, and applying CNRs
// implements cron.Job interface
func (c *controller) Run() {
	// get fresh valid nodegroups and in progress CNRs from the APIServer. These are not cached
	nodeGroups := c.validNodeGroups()
	inProgressCNRs := c.inProgressCNRs()

	// Filter out any nodegroups that match in progress CNRs. This is done by NodeGroup (ASG) name
	if len(inProgressCNRs.Items) == 0 {
		klog.V(2).Infoln("no active CNRs to wait for")
	} else {
		nodeGroups = c.dropInProgressNodeGroups(nodeGroups, inProgressCNRs)
	}

	// observer the changes using the remaining nodegroups. This is stateless and will pickup changes again if restarted
	changedNodeGroups := c.observeChanges(nodeGroups)
	if len(changedNodeGroups) == 0 {
		klog.V(2).Infoln("all nodegroups up to date. next check in", c.CheckInterval)
		return
	}

	klog.V(3).Infof("listing all %d nodegroups and nodes changed this run", len(changedNodeGroups))
	for _, nodeGroup := range changedNodeGroups {
		klog.V(2).Infof("nodegroup %q out of date", nodeGroup.NodeGroup.Name)
		for _, node := range nodeGroup.List {
			klog.V(2).Infof("for node %q", node.Name)
		}
	}

	// query cluster-autoscaler to check if it's safe to start a new CNR
	if !c.checkIfSafeToStartCycle() {
		return
	}

	// wait for the desired amount to allow any in progress changes to batch up
	klog.V(3).Infof("waiting for %v to allow changes to settle", c.WaitInterval)
	select {
	case <-time.After(c.WaitInterval):
		klog.V(3).Infof("applying %d CNRs", len(changedNodeGroups))
		c.createCNRs(changedNodeGroups)
		if c.RunOnce {
			klog.V(3).Infoln("done creating CNRs after runOnce. exiting")
		} else {
			klog.V(3).Infoln("done creating CNRs.. next check in", c.CheckInterval)
		}
	case <-c.stopCh:
		return
	}
}

// RunForever runs the Run on the cron loop until c.stopCh channel is closed
func (c *controller) RunForever() {
	// initial forced run
	if c.RunImmediately {
		klog.V(3).Infoln("running immediately as specified in cli config")
		c.Run()
	}

	klog.V(3).Infoln("will run at", c.nextRunTime())

	ticker := time.NewTicker(c.CheckInterval)
	for {
		select {
		case <-ticker.C:
			klog.V(3).Infoln("running check loop")
			c.Run()

			klog.V(3).Infoln("will run again at", c.nextRunTime())
		case <-c.stopCh:
			ticker.Stop()
			return
		}
	}
}
