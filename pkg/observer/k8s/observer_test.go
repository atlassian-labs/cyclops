package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/atlassian-labs/cyclops/pkg/observer"
	"github.com/atlassian-labs/cyclops/pkg/test"
)

func TestK8sObserver_Changed(t *testing.T) {
	scenarioUpToDate := test.BuildTestScenario(test.ScenarioOpts{
		Keys:         []string{"a", "b"},
		NodeCount:    2,
		PodCount:     1,
		PodsUpToDate: map[string]bool{"a": true, "b": true},
	})
	scenarioUpToDateOne := test.FlattenScenario(scenarioUpToDate, "a")
	scenarioUpToDateAll := test.FlattenScenario(scenarioUpToDate)

	scenarioOutOfDate := test.BuildTestScenario(test.ScenarioOpts{
		Keys:         []string{"a", "b"},
		NodeCount:    2,
		PodCount:     1,
		PodsUpToDate: map[string]bool{"a": false, "b": true},
	})
	scenarioOutOfDateOne := test.FlattenScenario(scenarioOutOfDate, "a")
	scenarioOutOfDateAll := test.FlattenScenario(scenarioOutOfDate, "a", "b")

	scenarioOutOfDateMany := test.BuildTestScenario(test.ScenarioOpts{
		Keys:         []string{"a", "b", "c", "d"},
		NodeCount:    2,
		PodCount:     2,
		PodsUpToDate: map[string]bool{"a": false, "b": false, "c": false, "d": false},
	})
	scenarioOutOfDateManyFlat := test.FlattenScenario(scenarioOutOfDateMany, "a", "b", "c", "d")

	tests := []struct {
		name             string
		nodegroups       []*atlassianv1.NodeGroup
		nodes            []*corev1.Node
		pods             []*corev1.Pod
		dss              []*appsv1.DaemonSet
		crs              []*appsv1.ControllerRevision
		expectNodeGroups []*observer.ListedNodeGroups
		testLen          int
	}{
		{
			"test one node group up to date",
			scenarioUpToDateOne.Nodegroups,
			scenarioUpToDateOne.Nodes,
			scenarioUpToDateOne.Pods,
			scenarioUpToDateOne.Daemonsets,
			scenarioUpToDateOne.ControllerRevisions,
			nil,
			-1,
		},
		{
			"test all node group up to date",
			scenarioUpToDateAll.Nodegroups,
			scenarioUpToDateAll.Nodes,
			scenarioUpToDateAll.Pods,
			scenarioUpToDateAll.Daemonsets,
			scenarioUpToDateAll.ControllerRevisions,
			nil,
			-1,
		},
		{
			"test 1 node group out of date",
			scenarioOutOfDateOne.Nodegroups,
			scenarioOutOfDateOne.Nodes,
			scenarioOutOfDateOne.Pods,
			scenarioOutOfDateOne.Daemonsets,
			scenarioOutOfDateOne.ControllerRevisions,
			[]*observer.ListedNodeGroups{
				{
					NodeGroup: scenarioOutOfDate.Nodegroups["a"],
					List:      scenarioOutOfDate.Nodes["a"][:1],
					Reason:    `pod "pod-a-0" hash "oldhash" is not up to date with latest daemonset controller revision "cr-latest-a" hash "latesthash" rev 2`,
				},
			},
			-1,
		},
		{
			"test 2 node group A out of date",
			scenarioOutOfDateAll.Nodegroups,
			scenarioOutOfDateAll.Nodes,
			scenarioOutOfDateAll.Pods,
			scenarioOutOfDateAll.Daemonsets,
			scenarioOutOfDateAll.ControllerRevisions,
			[]*observer.ListedNodeGroups{
				{
					NodeGroup: scenarioOutOfDate.Nodegroups["a"],
					List:      scenarioOutOfDate.Nodes["a"][:1],
					Reason:    `pod "pod-a-0" hash "oldhash" is not up to date with latest daemonset controller revision "cr-latest-a" hash "latesthash" rev 2`,
				},
			},
			-1,
		},
		{
			"test many node group both all of date",
			scenarioOutOfDateManyFlat.Nodegroups,
			scenarioOutOfDateManyFlat.Nodes,
			scenarioOutOfDateManyFlat.Pods,
			scenarioOutOfDateManyFlat.Daemonsets,
			scenarioOutOfDateManyFlat.ControllerRevisions,
			nil,
			4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeLister := test.NewTestNodeWatcher(tt.nodes, test.NodeListerOptions{ReturnErrorOnList: false})
			podsLister := test.NewTestPodWatcher(tt.pods, test.PodListerOptions{ReturnErrorOnList: false})

			dsCache := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			for i := range tt.dss {
				_ = dsCache.Add(tt.dss[i])
			}
			dsLister := k8s.NewCachedDaemonSetList(dsCache)

			crCache := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			for i := range tt.crs {
				_ = crCache.Add(tt.crs[i])
			}
			crLister := k8s.NewCachedControllerRevisionList(crCache)

			obs := NewObserver(nodeLister, podsLister, dsLister, crLister)

			var nodegroups atlassianv1.NodeGroupList
			for i := range tt.nodegroups {
				nodegroups.Items = append(nodegroups.Items, *tt.nodegroups[i])
			}

			listed := obs.Changed(&nodegroups)
			if tt.testLen >= 0 {
				assert.Equal(t, tt.testLen, len(listed))
			} else {
				assert.Equal(t, tt.expectNodeGroups, listed)
			}
		})
	}
}
