package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
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
		"Number of CycleNodeRequests in the cluster by phase",
		[]string{"phase"},
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
	cycleNodeRequestList *v1.CycleNodeRequestList
	cycleNodeStatusList  *v1.CycleNodeStatusList
}

func (c cyclopsCollector) fetch() {
	// List the cycleNodeRequests in the cluster
	listOptions := client.ListOptions{
		Namespace: c.namespace,
	}
	err := c.client.List(context.TODO(), c.cycleNodeRequestList, &listOptions)
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
	requestPhaseCounts := make(map[string]int)
	for _, cycleNodeRequest := range c.cycleNodeRequestList.Items {
		requestPhaseCounts[string(cycleNodeRequest.Status.Phase)]++
	}
	for phase, count := range requestPhaseCounts {
		ch <- prometheus.MustNewConstMetric(
			CycleNodeRequestsByPhase,
			prometheus.GaugeValue,
			float64(count),
			phase,
		)
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
