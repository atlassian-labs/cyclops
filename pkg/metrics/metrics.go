package metrics

import (
	"context"
	"fmt"
	"strings"
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
	// NodeGroupInfo provides static information about nodegroups
	NodeGroupInfo = prometheus.NewDesc(
		fmt.Sprintf("%v_node_group_info", namespace),
		"Static information about nodegroups in the cluster",
		[]string{
			"nodegroup_name",
			"nodegroups_list",
			"node_selector",           
			"concurrency",
			"method",
			"max_failed_cnrs",
			"skip_missing_named_nodes",
			"skip_initial_health_checks",
			"skip_pre_termination_checks",
			"priority",
		},
		nil,
	)
)

var (
	// AnnotationAddSuccess tracks successful annotation additions
	// Note: Using only nodegroup label to keep cardinality low (consistent with existing Cyclops metrics)
	AnnotationAddSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      fmt.Sprintf("%v_annotation_add_success_total", namespace),
			Help:      "Total number of successful cluster-autoscaler scale-down-disabled annotation additions",
		},
		[]string{"nodegroup"},
	)

	// AnnotationAddFailure tracks failed annotation additions
	AnnotationAddFailure = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      fmt.Sprintf("%v_annotation_add_failure_total", namespace),
			Help:      "Total number of failed cluster-autoscaler scale-down-disabled annotation additions",
		},
		[]string{"nodegroup", "error_type"},
	)

	// AnnotationRemoveSuccess tracks successful annotation removals
	AnnotationRemoveSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      fmt.Sprintf("%v_annotation_remove_success_total", namespace),
			Help:      "Total number of successful cluster-autoscaler scale-down-disabled annotation removals",
		},
		[]string{"nodegroup"},
	)

	// AnnotationRemoveFailure tracks failed annotation removals
	AnnotationRemoveFailure = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      fmt.Sprintf("%v_annotation_remove_failure_total", namespace),
			Help:      "Total number of failed cluster-autoscaler scale-down-disabled annotation removals",
		},
		[]string{"nodegroup", "error_type"},
	)

	// NodesWithAnnotation tracks current nodes with the annotation
	// Note: Includes node_name here because we need to track which specific nodes currently have the annotation
	// Cardinality is bounded by the number of nodes with annotations (typically small)
	NodesWithAnnotation = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      fmt.Sprintf("%v_nodes_with_scale_down_disabled_annotation", namespace),
			Help:      "Current number of nodes with cluster-autoscaler scale-down-disabled annotation (1 = has annotation, 0 = does not have annotation)",
		},
		[]string{"nodegroup", "node_name"},
	)

	// AnnotationCleanupAttempts tracks cleanup operation attempts
	AnnotationCleanupAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      fmt.Sprintf("%v_annotation_cleanup_attempts_total", namespace),
			Help:      "Total number of annotation cleanup attempts",
		},
		[]string{"nodegroup", "result"}, // result: "success", "partial_failure", "failure"
	)

	// AnnotationCleanupDuration tracks cleanup operation duration
	AnnotationCleanupDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:      fmt.Sprintf("%v_annotation_cleanup_duration_seconds", namespace),
			Help:      "Duration of annotation cleanup operations in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~51s
		},
		[]string{"nodegroup"},
	)

	// --- Node Cleanup Controller metrics (safety-net reconciler) ---

	// NodeCleanupAnnotationsRemoved tracks how many stale annotations the node
	// cleanup controller has removed. A non-zero rate means the normal CNR
	// lifecycle failed to clean up after itself.
	NodeCleanupAnnotationsRemoved = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%v_node_cleanup_annotations_removed_total", namespace),
			Help: "Total number of stale scale-down-disabled annotations removed by the node cleanup controller",
		},
	)

	// NodeCleanupReconciles tracks reconcile outcomes for the node cleanup controller.
	// Labels:
	//   result: "cleaned"              – stale annotations were removed
	//           "active_cnr_skipped"   – node is covered by an active CNR, left alone
	//           "no_nodegroup_skipped" – node is not selected by any NodeGroup, left alone
	//           "error"                – reconcile failed with an error
	NodeCleanupReconciles = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: fmt.Sprintf("%v_node_cleanup_reconciles_total", namespace),
			Help: "Total number of node cleanup controller reconcile outcomes",
		},
		[]string{"result"},
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

	// Register annotation metrics
	metrics.Registry.MustRegister(
		AnnotationAddSuccess,
		AnnotationAddFailure,
		AnnotationRemoveSuccess,
		AnnotationRemoveFailure,
		NodesWithAnnotation,
		AnnotationCleanupAttempts,
		AnnotationCleanupDuration,
		NodeCleanupAnnotationsRemoved,
		NodeCleanupReconciles,
	)

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
	ch <- NodeGroupInfo
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

    for _, nodegroup := range c.nodeGroupList.Items {
        ch <- prometheus.MustNewConstMetric(
            NodeGroupInfo,
            prometheus.GaugeValue,
            1.0,
			nodegroup.Spec.NodeGroupName,
			strings.Join(nodegroup.Spec.NodeGroupsList, ","),
			nodegroup.Spec.NodeSelector.String(),
			fmt.Sprintf("%d", nodegroup.Spec.CycleSettings.Concurrency),
			string(nodegroup.Spec.CycleSettings.Method),
			fmt.Sprintf("%d", nodegroup.Spec.MaxFailedCycleNodeRequests),
			fmt.Sprintf("%t", nodegroup.Spec.ValidationOptions.SkipMissingNamedNodes),
			fmt.Sprintf("%t", nodegroup.Spec.SkipInitialHealthChecks),
			fmt.Sprintf("%t", nodegroup.Spec.SkipPreTerminationChecks),
			fmt.Sprintf("%d", nodegroup.Spec.Priority),
        )
    }
}
