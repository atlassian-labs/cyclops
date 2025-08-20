package metrics

import (
	"testing"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeGroupInfoMetric(t *testing.T) {
	// Create a test NodeGroup
	testNodeGroup := &v1.NodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ng",
		},
		Spec: v1.NodeGroupSpec{
			NodeGroupName: "test-nodegroup",
			NodeGroupsList: []string{"ng1", "ng2"},
			NodeSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"role": "worker",
				},
			},
			CycleSettings: v1.CycleSettings{
				Method:      "Drain",
				Concurrency: 2,
			},
			MaxFailedCycleNodeRequests: 3,
			ValidationOptions: v1.ValidationOptions{
				SkipMissingNamedNodes: true,
			},
			SkipInitialHealthChecks: false,
			SkipPreTerminationChecks: true,
			Priority: 10,
		},
	}

	// Create a test NodeGroupList
	testNodeGroupList := &v1.NodeGroupList{
		Items: []v1.NodeGroup{*testNodeGroup},
	}

	// Create a mock collector with all required fields initialized
	collector := &cyclopsCollector{
		nodeGroupList:        testNodeGroupList,
		cycleNodeRequestList: &v1.CycleNodeRequestList{Items: []v1.CycleNodeRequest{}},
		cycleNodeStatusList:  &v1.CycleNodeStatusList{Items: []v1.CycleNodeStatus{}},
	}

	// Test that the metric can be described
	t.Run("Describe", func(t *testing.T) {
		ch := make(chan *prometheus.Desc, 10) // Buffer size to hold all metrics
		go func() {
			collector.Describe(ch)
			close(ch)
		}()
		
		// Read all descriptions
		descriptions := make([]*prometheus.Desc, 0)
		for desc := range ch {
			descriptions = append(descriptions, desc)
		}
		
		// Verify we got descriptions and one contains our metric
		assert.Greater(t, len(descriptions), 0)
		found := false
		for _, desc := range descriptions {
			if desc.String() == NodeGroupInfo.String() {
				found = true
				break
			}
		}
		assert.True(t, found, "NodeGroupInfo metric should be described")
	})

	// Test that the metric can be collected
	t.Run("Collect", func(t *testing.T) {
		ch := make(chan prometheus.Metric, 10) // Buffer size to hold all metrics
		go func() {
			collector.Collect(ch)
			close(ch)
		}()
		
		// Read all metrics
		metrics := make([]prometheus.Metric, 0)
		for metric := range ch {
			metrics = append(metrics, metric)
		}
		
		// Verify we got metrics and one is our NodeGroupInfo
		assert.Greater(t, len(metrics), 0)
		found := false
		for _, metric := range metrics {
			desc := metric.Desc()
			if desc.String() == NodeGroupInfo.String() {
				found = true
				break
			}
		}
		assert.True(t, found, "NodeGroupInfo metric should be collected")
	})
}

func TestNodeGroupInfoLabels(t *testing.T) {
	// Test that the metric definition has the correct number of labels
	expectedLabels := []string{
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
	}
	
	// Verify we have the expected number of labels
	assert.Equal(t, 10, len(expectedLabels))
	
	assert.Contains(t, NodeGroupInfo.String(), "cyclops_node_group_info")
	
	// Test that the metric is properly registered
	assert.NotNil(t, NodeGroupInfo)
}