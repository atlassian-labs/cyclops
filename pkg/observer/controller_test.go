package observer

import (
	"context"
	"fmt"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/test"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_unionNodes(t *testing.T) {
	nodesA := test.BuildTestNodes(10, test.NodeOpts{})
	nodesB := test.BuildTestNodes(10, test.NodeOpts{})

	union := unionNodes(nodesA, nodesB)
	assert.ElementsMatch(t, append(nodesA, nodesB...), union)

	doubledA := append(nodesA, nodesA...)
	union2 := unionNodes(doubledA, nodesB)
	assert.ElementsMatch(t, append(nodesA, nodesB...), union2)
}

func TestController_validNodeGroups(t *testing.T) {
	scenarioOk := test.BuildTestScenario(test.ScenarioOpts{
		Keys:         []string{"a", "b", "c"},
		NodeCount:    1,
		PodCount:     1,
		PodsUpToDate: map[string]bool{"a": true, "b": true, "c": true},
	}).Flatten()

	scenarioBad := test.BuildTestScenario(test.ScenarioOpts{
		Keys:         []string{"a", "b"},
		NodeCount:    3,
		PodCount:     5,
		PodsUpToDate: map[string]bool{"a": true, "b": true},
	})
	scenarioBad.Nodegroups["a"].Spec.CycleSettings.Concurrency = 0
	scenarioBadFlat := scenarioBad.Flatten()

	scenarioBadAll := test.BuildTestScenario(test.ScenarioOpts{
		Keys:         []string{"a", "b"},
		NodeCount:    3,
		PodCount:     5,
		PodsUpToDate: map[string]bool{"a": true, "b": true},
	})
	scenarioBadAll.Nodegroups["a"].Spec.CycleSettings.Concurrency = 0
	scenarioBadAll.Nodegroups["b"].Spec.CycleSettings.Concurrency = 0
	scenarioBadAllFlat := scenarioBadAll.Flatten()

	tests := []struct {
		name     string
		scenario *test.FlatScenario
		expect   []*atlassianv1.NodeGroup
	}{
		{
			"test all okay",
			scenarioOk,
			scenarioOk.Nodegroups,
		},
		{
			"test not all okay",
			scenarioBadFlat,
			[]*atlassianv1.NodeGroup{scenarioBad.Nodegroups["b"]},
		},
		{
			"test all not okay",
			scenarioBadAllFlat,
			[]*atlassianv1.NodeGroup{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, _ := atlassianv1.SchemeBuilder.Build()
			var objects []runtime.Object
			for i := range tt.scenario.Nodegroups {
				tt.scenario.Nodegroups[i].Spec.CycleSettings.Concurrency = 1
				objects = append(objects, tt.scenario.Nodegroups[i])
			}
			client := NewFakeClientWithScheme(scheme, objects...)

			var ngList atlassianv1.NodeGroupList
			_ = client.List(context.TODO(), &ngList)
			assert.Equal(t, len(tt.scenario.Nodegroups), len(ngList.Items))

			nodeLister := test.NewTestNodeWatcher(tt.scenario.Nodes, test.NodeListerOptions{ReturnErrorOnList: false})

			controller := controller{
				client,
				nil,
				nodeLister,
				map[string]Observer{"k8s": nil},
				[]timedKey{{key: "k8s", duration: 0}},
				nil,
				Options{},
			}

			ng := controller.validNodeGroups()
			assert.ElementsMatch(t, ngList.Items, ng.Items)
		})
	}
}

func Test_inProgressCNRs(t *testing.T) {

	var allInProgress []atlassianv1.CycleNodeRequest
	for i := 0; i < 10; i++ {
		allInProgress = append(allInProgress, atlassianv1.CycleNodeRequest{
			ObjectMeta: v1.ObjectMeta{
				Name:      fmt.Sprint("test-in-progress-", i),
				Namespace: "kube-system",
			},
			Spec: atlassianv1.CycleNodeRequestSpec{
				NodeGroupName: "test",
				CycleSettings: atlassianv1.CycleSettings{
					Method:      "Drain",
					Concurrency: 1,
				},
			},
		})
	}

	var allSuccessful []atlassianv1.CycleNodeRequest
	for i := 0; i < 10; i++ {
		allSuccessful = append(allSuccessful, atlassianv1.CycleNodeRequest{
			ObjectMeta: v1.ObjectMeta{
				Name:      fmt.Sprint("test-successful-", i),
				Namespace: "kube-system",
			},
			Spec: atlassianv1.CycleNodeRequestSpec{
				NodeGroupName: "test",
				CycleSettings: atlassianv1.CycleSettings{
					Method:      "Drain",
					Concurrency: 1,
				},
			},
			Status: atlassianv1.CycleNodeRequestStatus{
				Phase: "Successful",
			},
		})
	}

	tests := []struct {
		name   string
		cnrs   []atlassianv1.CycleNodeRequest
		expect []atlassianv1.CycleNodeRequest
	}{
		{
			"test all in progress",
			allInProgress,
			allInProgress,
		},
		{
			"test all successful",
			allSuccessful,
			make([]atlassianv1.CycleNodeRequest, 0),
		},
		{
			"test half successful",
			append(allInProgress, allSuccessful...),
			allInProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, _ := atlassianv1.SchemeBuilder.Build()
			var objects []runtime.Object
			for i := range tt.cnrs {
				objects = append(objects, &tt.cnrs[i])
			}
			client := NewFakeClientWithScheme(scheme, objects...)

			c := controller{
				client: client,
				Options: Options{
					Namespace: "kube-system",
				},
			}

			got := c.inProgressCNRs()
			assert.ElementsMatch(t, tt.expect, got.Items)
		})
	}
}

func TestScenarios_dropInProgressNodeGroups(t *testing.T) {

	scenario := test.BuildTestScenario(test.ScenarioOpts{
		Keys:         []string{"a", "b", "c"},
		NodeCount:    1,
		PodCount:     1,
		PodsUpToDate: map[string]bool{"a": true, "b": true, "c": true},
	}).Flatten()

	scenarioMatches := test.BuildTestScenario(test.ScenarioOpts{
		Keys:         []string{"x"},
		NodeCount:    1,
		PodCount:     1,
		PodsUpToDate: map[string]bool{"x": true},
	}).Flatten()

	scenarioMixed := test.BuildTestScenario(test.ScenarioOpts{
		Keys:         []string{"a", "b", "x", "c"},
		NodeCount:    1,
		PodCount:     1,
		PodsUpToDate: map[string]bool{"a": true, "b": true, "x": true, "c": true},
	}).Flatten()

	var allInProgress []atlassianv1.CycleNodeRequest
	for i := 0; i < 3; i++ {
		allInProgress = append(allInProgress, atlassianv1.CycleNodeRequest{
			ObjectMeta: v1.ObjectMeta{
				Name:      fmt.Sprint("test-in-progress-", i),
				Namespace: "kube-system",
			},
			Spec: atlassianv1.CycleNodeRequestSpec{
				NodeGroupName: "nodegroup-x",
				CycleSettings: atlassianv1.CycleSettings{
					Method:      "Drain",
					Concurrency: 1,
				},
			},
		})
	}

	tests := []struct {
		name   string
		ng     atlassianv1.NodeGroupList
		cnrs   atlassianv1.CycleNodeRequestList
		expect atlassianv1.NodeGroupList
	}{
		{
			"test all in progress but don't match",
			scenario.NodeGroupList(),
			atlassianv1.CycleNodeRequestList{Items: allInProgress},
			scenario.NodeGroupList(),
		},
		{
			"test all in progress and all match",
			scenarioMatches.NodeGroupList(),
			atlassianv1.CycleNodeRequestList{Items: allInProgress},
			atlassianv1.NodeGroupList{},
		},
		{
			"test all in progress and mixed match",
			scenarioMixed.NodeGroupList(),
			atlassianv1.CycleNodeRequestList{Items: allInProgress},
			scenario.NodeGroupList(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, _ := atlassianv1.SchemeBuilder.Build()
			var objects []runtime.Object
			for i := range tt.cnrs.Items {
				objects = append(objects, &tt.cnrs.Items[i])
			}
			client := NewFakeClientWithScheme(scheme, objects...)

			c := controller{
				client: client,
				Options: Options{
					Namespace: "kube-system",
				},
				metrics: newMetrics(),
			}

			got := c.dropInProgressNodeGroups(tt.ng, tt.cnrs)
			assert.ElementsMatch(t, tt.expect.Items, got.Items)
		})
	}
}

func Test_dropInProgressNodeGroups(t *testing.T) {
	nodegroup := atlassianv1.NodeGroup{
		ObjectMeta: v1.ObjectMeta{
			Name:      "ng-1",
			Namespace: "kube-system",
		},
		Spec: atlassianv1.NodeGroupSpec{
			NodeGroupName: "ng-1",
		},
	}

	cnr1 := atlassianv1.CycleNodeRequest{
		ObjectMeta: v1.ObjectMeta{
			Name:      "cnr-1",
			Namespace: "kube-system",
		},
		Spec: atlassianv1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: atlassianv1.CycleNodeRequestStatus{
			Phase: atlassianv1.CycleNodeRequestPending,
		},
	}

	cnr2 := atlassianv1.CycleNodeRequest{
		ObjectMeta: v1.ObjectMeta{
			Name:      "cnr-2",
			Namespace: "kube-system",
		},
		Spec: atlassianv1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: atlassianv1.CycleNodeRequestStatus{
			Phase: atlassianv1.CycleNodeRequestFailed,
		},
	}

	tests := []struct {
		name          string
		threshold     int
		CNRs          []atlassianv1.CycleNodeRequest
		dropNodegroup bool
	}{
		{
			"test no CNRs for nodegroups",
			0,
			[]atlassianv1.CycleNodeRequest{},
			false,
		},
		{
			"test Pending CNR for nodegroup",
			0,
			[]atlassianv1.CycleNodeRequest{cnr1},
			true,
		},
		{
			"test less failed CNRs than threshold",
			2,
			[]atlassianv1.CycleNodeRequest{cnr2},
			false,
		},
		{
			"test same number of failed CNRs as threshold",
			1,
			[]atlassianv1.CycleNodeRequest{cnr2},
			false,
		},
		{
			"test more failed CNRs than threshold",
			0,
			[]atlassianv1.CycleNodeRequest{cnr2},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := controller{
				client:  nil,
				Options: Options{},
				metrics: newMetrics(),
			}

			nodegroup.Spec.MaxFailedCycleNodeRequests = uint(tt.threshold)

			nodegroupList := atlassianv1.NodeGroupList{
				Items: []atlassianv1.NodeGroup{nodegroup},
			}

			cnrList := atlassianv1.CycleNodeRequestList{
				Items: tt.CNRs,
			}

			got := c.dropInProgressNodeGroups(nodegroupList, cnrList)

			if tt.dropNodegroup {
				assert.Len(t, got.Items, 0)
			} else {
				assert.Len(t, got.Items, 1)
			}
		})
	}
}

func Test_sameNodeGroups(t *testing.T) {
	tests := []struct {
		name   string
		groupA []string
		groupB []string
		expect bool
	}{
		{
			"pass case with same order",
			[]string{"ingress-us-west-2a", "ingress-us-west-2b", "ingress-us-west-2c"},
			[]string{"ingress-us-west-2a", "ingress-us-west-2b", "ingress-us-west-2c"},
			true,
		},
		{
			"pass case with different order",
			[]string{"ingress-us-west-2a", "ingress-us-west-2b", "ingress-us-west-2c"},
			[]string{"ingress-us-west-2b", "ingress-us-west-2c", "ingress-us-west-2a"},
			true,
		},
		{
			"failure case with different length",
			[]string{"ingress-us-west-2a", "ingress-us-west-2b", "ingress-us-west-2c"},
			[]string{"ingress-us-west-2b", "ingress-us-west-2c"},
			false,
		},
		{
			"failure case with different items",
			[]string{"ingress-us-west-2a", "ingress-us-west-2b", "ingress-us-west-2c"},
			[]string{"ingress-us-west-2b", "ingress-us-west-2c", "system"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sameNodeGroups(tt.groupA, tt.groupB)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func NewFakeClientWithScheme(clientScheme *runtime.Scheme, initObjs ...runtime.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(clientScheme).WithRuntimeObjects(initObjs...).Build()
}
