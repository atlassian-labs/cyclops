package metrics

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const namespace = "cyclops"

var (
	// CycleNodeRequests is the number of CycleNodeRequests in the cluster
	CycleNodeRequests = prometheus.NewDesc(
		fmt.Sprintf("%v_cycle_node_requests", namespace),
		"Number of CycleNodeRequests in the cluster",
		[]string{},
		nil,
	)
	// CycleNodeRequestsByPhase is the number of CycleNodeRequests in the cluster by phase
	CycleNodeRequestsByPhase = prometheus.NewDesc(
		fmt.Sprintf("%v_cycle_node_requests_by_phase", namespace),
		"Number of CycleNodeRequests in the cluster by phase for each nodegroup",
		[]string{"phase", "nodegroup"},
		nil,
	)
	// CycleNodeStatuses is the number of CycleNodeStatuses in the cluster
	CycleNodeStatuses = prometheus.NewDesc(
		fmt.Sprintf("%v_cycle_node_status", namespace),
		"Number of CycleNodeStatuses in the cluster",
		[]string{},
		nil,
	)
	// CycleNodeStatusesByPhase is the number of CycleNodeStatuses in the cluster by phase
	CycleNodeStatusesByPhase = prometheus.NewDesc(
		fmt.Sprintf("%v_cycle_node_status_by_phase", namespace),
		"Number of CycleNodeStatuses in the cluster by phase",
		[]string{"phase"},
		nil,
	)
)

// Register registers the custom metrics with prometheus
func Register(client client.Client, logger logr.Logger, namespace string) {
	collector := cyclopsCollector{
		client:               client,
		logger:               logger,
		namespace:            namespace,
		nodeGroupList:        &v1.NodeGroupList{},
		cycleNodeRequestList: &v1.CycleNodeRequestList{},
		cycleNodeStatusList:  &v1.CycleNodeStatusList{},
	}
	metrics.Registry.MustRegister(collector)

	go func() {
		for {
			collector.fetch()
			time.Sleep(10 * time.Second)
		}
	}()
}

type cyclopsCollector struct {
	client               client.Client
	logger               logr.Logger
	namespace            string
	nodeGroupList        *v1.NodeGroupList
	cycleNodeRequestList *v1.CycleNodeRequestList
	cycleNodeStatusList  *v1.CycleNodeStatusList
}

func (c cyclopsCollector) fetch() {
	// List the cycleNodeRequests in the cluster
	listOptions := client.ListOptions{
		Namespace: c.namespace,
	}

	// NodeGroup is a cluster scoped resource
	err := c.client.List(context.TODO(), c.nodeGroupList, &client.ListOptions{})
	if err != nil {
		c.logger.Error(err, "unable to list NodeGroups for metrics")
		return
	}

	err = c.client.List(context.TODO(), c.cycleNodeRequestList, &listOptions)
	if err != nil {
		c.logger.Error(err, "unable to list CycleNodeRequests for metrics")
		return
	}

	err = c.client.List(context.TODO(), c.cycleNodeStatusList, &listOptions)
	if err != nil {
		c.logger.Error(err, "unable to list CycleNodeStatuses for metrics")
		return
	}
}

func (c cyclopsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- CycleNodeRequests
	ch <- CycleNodeRequestsByPhase
	ch <- CycleNodeStatuses
	ch <- CycleNodeStatusesByPhase
}

func (c cyclopsCollector) Collect(ch chan<- prometheus.Metric) {
	// map counting CNRs in each phase for each nodegroup
	requestPhaseCounts := make(map[string]map[string]int)
	requestPhaseCounts[""] = make(map[string]int)

	for _, nodegroup := range c.nodeGroupList.Items {
		requestPhaseCounts[nodegroup.Name] = make(map[string]int)
	}

	for _, cycleNodeRequest := range c.cycleNodeRequestList.Items {
		partOfANodegroup := false

		for _, nodegroup := range c.nodeGroupList.Items {
			if cycleNodeRequest.IsFromNodeGroup(nodegroup) {
				requestPhaseCounts[nodegroup.Name][string(cycleNodeRequest.Status.Phase)]++
				partOfANodegroup = true
				break
			}
		}

		// Account manually created CNRs which do not share the same defined
		// node group names as any NodeGroups.
		if !partOfANodegroup {
			requestPhaseCounts[""][string(cycleNodeRequest.Status.Phase)]++
		}
	}

	for nodegroupName, cycleNodeRequestsByPhase := range requestPhaseCounts {
		for phase, count := range cycleNodeRequestsByPhase {
			ch <- prometheus.MustNewConstMetric(
				CycleNodeRequestsByPhase,
				prometheus.GaugeValue,
				float64(count),
				phase,
				nodegroupName,
			)
		}
	}

	ch <- prometheus.MustNewConstMetric(
		CycleNodeRequests,
		prometheus.GaugeValue,
		float64(len(c.cycleNodeRequestList.Items)),
	)

	statusPhaseCounts := make(map[string]int)

	for _, cycleNodeStatus := range c.cycleNodeStatusList.Items {
		statusPhaseCounts[string(cycleNodeStatus.Status.Phase)]++
	}

	for phase, count := range statusPhaseCounts {
		ch <- prometheus.MustNewConstMetric(
			CycleNodeStatusesByPhase,
			prometheus.GaugeValue,
			float64(count),
			phase,
		)
	}

	ch <- prometheus.MustNewConstMetric(
		CycleNodeStatuses,
		prometheus.GaugeValue,
		float64(len(c.cycleNodeStatusList.Items)),
	)
}
