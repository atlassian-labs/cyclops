package observer

import (
	"context"
	"fmt"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/generation"
	"github.com/atlassian-labs/cyclops/pkg/test"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
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

func NewFakeClientWithScheme(clientScheme *runtime.Scheme, initObjs ...runtime.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(clientScheme).WithRuntimeObjects(initObjs...).Build()
}

// ---- Priority system tests ----

// testObserver implements Observer and returns changes only for NodeGroups present in the provided list
type testObserver struct{ changed map[string]*ListedNodeGroups }

func (t testObserver) Changed(list *atlassianv1.NodeGroupList) []*ListedNodeGroups {
    var out []*ListedNodeGroups
    if list == nil {
        for _, lg := range t.changed {
            out = append(out, lg)
        }
        return out
    }
    for i := range list.Items {
        name := list.Items[i].Name
        if lg, ok := t.changed[name]; ok {
            out = append(out, lg)
        }
    }
    return out
}

func newPriorityControllerForTest(t *testing.T, objects []runtime.Object, nodes []*corev1.Node, obs Observer) *controller {
    t.Helper()
    scheme, _ := atlassianv1.SchemeBuilder.Build()
    c := NewFakeClientWithScheme(scheme, objects...)

    return &controller{
        client:         c,
        observers:      map[string]Observer{"test": obs},
        nodeLister:     test.NewTestNodeWatcher(nodes, test.NodeListerOptions{ReturnErrorOnList: false}),
        optimisedOrder: []timedKey{{key: "test"}},
        metrics:        newMetrics(),
        Options: Options{
            Namespace:    "kube-system",
            WaitInterval: 0,
        },
    }
}

func buildListed(ng *atlassianv1.NodeGroup, nodeNames ...string) *ListedNodeGroups {
    var nodes []*corev1.Node
    for _, n := range nodeNames {
        nodes = append(nodes, &corev1.Node{ObjectMeta: v1.ObjectMeta{Name: n}})
    }
    return &ListedNodeGroups{NodeGroup: ng, List: nodes, Reason: "test"}
}

func TestPriority_AllSamePriority_CreatesAll(t *testing.T) {
    scenario := test.BuildTestScenario(test.ScenarioOpts{Keys: []string{"a", "b"}, NodeCount: 1, PodCount: 1}).Flatten()
    for _, ng := range scenario.Nodegroups {
        ng.Spec.CycleSettings.Concurrency = 1
        ng.Spec.Priority = 1
    }

    var objects []runtime.Object
    for i := range scenario.Nodegroups {
        objects = append(objects, scenario.Nodegroups[i])
    }

    obs := testObserver{changed: map[string]*ListedNodeGroups{
        scenario.Nodegroups[0].Name: buildListed(scenario.Nodegroups[0], scenario.Nodes[0].Name),
        scenario.Nodegroups[1].Name: buildListed(scenario.Nodegroups[1], scenario.Nodes[0].Name),
    }}
    ctrl := newPriorityControllerForTest(t, objects, scenario.Nodes, obs)

    ctrl.Run()

    lst, _ := generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    assert.Equal(t, 2, len(lst.Items))
}

func TestPriority_FirstFailed_BlocksSecond(t *testing.T) {
    scenario := test.BuildTestScenario(test.ScenarioOpts{Keys: []string{"a", "b"}, NodeCount: 1, PodCount: 1}).Flatten()
    a := scenario.Nodegroups[0]
    b := scenario.Nodegroups[1]
    a.Spec.CycleSettings.Concurrency = 1
    b.Spec.CycleSettings.Concurrency = 1
    a.Spec.Priority = 0
    b.Spec.Priority = 10

    var objects []runtime.Object
    objects = append(objects, a, b)

    obs := testObserver{changed: map[string]*ListedNodeGroups{
        a.Name: buildListed(a, scenario.Nodes[0].Name),
        b.Name: buildListed(b, scenario.Nodes[0].Name),
    }}
    ctrl := newPriorityControllerForTest(t, objects, scenario.Nodes, obs)

    // Create A
    ctrl.Run()
    lst1, _ := generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    assert.Equal(t, 1, len(lst1.Items))

    // Mark A as Failed
    var cnrA atlassianv1.CycleNodeRequest
    _ = ctrl.client.Get(context.TODO(), client.ObjectKey{Namespace: ctrl.Namespace, Name: lst1.Items[0].Name}, &cnrA)
    cnrA.Status.Phase = atlassianv1.CycleNodeRequestFailed
    _ = ctrl.client.Update(context.TODO(), &cnrA)

    // Subsequent runs should never create B while A is Failed (treated as in-progress)
    ctrl.Run()
    ctrl.Run()
    lst2, _ := generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    assert.Equal(t, 1, len(lst2.Items))
}

func TestPriority_DefaultingNegativeAsZero_Sequential(t *testing.T) {
    scenario := test.BuildTestScenario(test.ScenarioOpts{Keys: []string{"a", "b"}, NodeCount: 1, PodCount: 1}).Flatten()
    a := scenario.Nodegroups[0]
    b := scenario.Nodegroups[1]
    a.Spec.CycleSettings.Concurrency = 1
    b.Spec.CycleSettings.Concurrency = 1
    a.Spec.Priority = -5 // should default to 0
    b.Spec.Priority = 1

    var objects []runtime.Object
    objects = append(objects, a, b)

    obs := testObserver{changed: map[string]*ListedNodeGroups{
        a.Name: buildListed(a, scenario.Nodes[0].Name),
        b.Name: buildListed(b, scenario.Nodes[0].Name),
    }}
    ctrl := newPriorityControllerForTest(t, objects, scenario.Nodes, obs)

    // Run 1: only A should be created
    ctrl.Run()
    lst1, _ := generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    assert.Equal(t, 1, len(lst1.Items))

    // Mark A Successful, then B should be created
    var cnrA atlassianv1.CycleNodeRequest
    _ = ctrl.client.Get(context.TODO(), client.ObjectKey{Namespace: ctrl.Namespace, Name: lst1.Items[0].Name}, &cnrA)
    cnrA.Status.Phase = atlassianv1.CycleNodeRequestSuccessful
    _ = ctrl.client.Update(context.TODO(), &cnrA)

    ctrl.Run()
    lst2, _ := generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    assert.Equal(t, 2, len(lst2.Items))
}

func TestPriority_ThreeLevels_Sequential(t *testing.T) {
    scenario := test.BuildTestScenario(test.ScenarioOpts{Keys: []string{"a", "b", "c"}, NodeCount: 1, PodCount: 1}).Flatten()
    a := scenario.Nodegroups[0]
    b := scenario.Nodegroups[1]
    c := scenario.Nodegroups[2]
    a.Spec.CycleSettings.Concurrency = 1
    b.Spec.CycleSettings.Concurrency = 1
    c.Spec.CycleSettings.Concurrency = 1
    a.Spec.Priority = 0
    b.Spec.Priority = 10
    c.Spec.Priority = 20

    var objects []runtime.Object
    objects = append(objects, a, b, c)

    obs := testObserver{changed: map[string]*ListedNodeGroups{
        a.Name: buildListed(a, scenario.Nodes[0].Name),
        b.Name: buildListed(b, scenario.Nodes[0].Name),
        c.Name: buildListed(c, scenario.Nodes[0].Name),
    }}
    ctrl := newPriorityControllerForTest(t, objects, scenario.Nodes, obs)

    // Run 1: A only
    ctrl.Run()
    lst, _ := generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    assert.Equal(t, 1, len(lst.Items))

    // Success A
    var cnr atlassianv1.CycleNodeRequest
    _ = ctrl.client.Get(context.TODO(), client.ObjectKey{Namespace: ctrl.Namespace, Name: lst.Items[0].Name}, &cnr)
    cnr.Status.Phase = atlassianv1.CycleNodeRequestSuccessful
    _ = ctrl.client.Update(context.TODO(), &cnr)

    // Run 2: B only
    ctrl.Run()
    lst, _ = generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    assert.Equal(t, 2, len(lst.Items))

    // Success B
    var cnrB atlassianv1.CycleNodeRequest
    // find newly added (2nd item could be either order due to GenerateName; just update the non-successful one)
    for i := range lst.Items {
        if lst.Items[i].Status.Phase != atlassianv1.CycleNodeRequestSuccessful {
            _ = ctrl.client.Get(context.TODO(), client.ObjectKey{Namespace: ctrl.Namespace, Name: lst.Items[i].Name}, &cnrB)
            cnrB.Status.Phase = atlassianv1.CycleNodeRequestSuccessful
            _ = ctrl.client.Update(context.TODO(), &cnrB)
            break
        }
    }

    // Run 3: C created
    ctrl.Run()
    lst, _ = generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    assert.Equal(t, 3, len(lst.Items))
}

func TestPriority_GlobalGuard_BlocksOnExternalLowerPriority(t *testing.T) {
    // Changed contains only B (p1). Seed an external lower-priority NG X (p0) with an in-progress CNR
    scenario := test.BuildTestScenario(test.ScenarioOpts{Keys: []string{"b", "x"}, NodeCount: 1, PodCount: 1}).Flatten()
    var b, x *atlassianv1.NodeGroup
    for _, ng := range scenario.Nodegroups {
		switch ng.Name {
		case "b":
			b = ng
		case "x":
			x = ng
		}
	}
    b.Spec.CycleSettings.Concurrency = 1
    x.Spec.CycleSettings.Concurrency = 1
    b.Spec.Priority = 1
    x.Spec.Priority = 0

    cnrX := atlassianv1.CycleNodeRequest{
        ObjectMeta: v1.ObjectMeta{Name: "cnr-x", Namespace: "kube-system"},
        Spec: atlassianv1.CycleNodeRequestSpec{
            NodeGroupName: x.Spec.NodeGroupName,
            CycleSettings: atlassianv1.CycleSettings{Method: "Drain", Concurrency: 1},
        },
        Status: atlassianv1.CycleNodeRequestStatus{Phase: atlassianv1.CycleNodeRequestPending},
    }

    var objects []runtime.Object
    objects = append(objects, b, x, &cnrX)

    obs := testObserver{changed: map[string]*ListedNodeGroups{
        b.Name: buildListed(b, scenario.Nodes[0].Name),
    }}
    ctrl := newPriorityControllerForTest(t, objects, scenario.Nodes, obs)

    // Run: guard should block because X p0 has in-progress CNR
    ctrl.Run()
    lst, _ := generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    // Only the seeded CNR for X should exist
    assert.Equal(t, 1, len(lst.Items))
    assert.Equal(t, "cnr-x", lst.Items[0].Name)
}

func TestPriority_DropInProgress_OnSameNodeGroup_NoCreate(t *testing.T) {
    scenario := test.BuildTestScenario(test.ScenarioOpts{Keys: []string{"a"}, NodeCount: 1, PodCount: 1}).Flatten()
    a := scenario.Nodegroups[0]
    a.Spec.CycleSettings.Concurrency = 1
    a.Spec.Priority = 0

    cnrA := atlassianv1.CycleNodeRequest{
        ObjectMeta: v1.ObjectMeta{Name: "cnr-a", Namespace: "kube-system"},
        Spec: atlassianv1.CycleNodeRequestSpec{
            NodeGroupName: a.Spec.NodeGroupName,
            CycleSettings: atlassianv1.CycleSettings{Method: "Drain", Concurrency: 1},
        },
        Status: atlassianv1.CycleNodeRequestStatus{Phase: atlassianv1.CycleNodeRequestPending},
    }

    var objects []runtime.Object
    objects = append(objects, a, &cnrA)

    obs := testObserver{changed: map[string]*ListedNodeGroups{
        a.Name: buildListed(a, scenario.Nodes[0].Name),
    }}
    ctrl := newPriorityControllerForTest(t, objects, scenario.Nodes, obs)

    // Because A already has an in-progress CNR, dropInProgressNodeGroups should remove it; no new CNR should be created
    ctrl.Run()
    lst, _ := generation.ListCNRs(ctrl.client, &client.ListOptions{Namespace: ctrl.Namespace})
    assert.Equal(t, 1, len(lst.Items))
    assert.Equal(t, "cnr-a", lst.Items[0].Name)
}

// ---- Unit tests for helpers ----

func Test_selectLowestPriorityNodeGroups_Empty(t *testing.T) {
    ctrl := &controller{metrics: newMetrics()}
    got := ctrl.selectLowestPriorityNodeGroups(nil)
    assert.Nil(t, got)
}

func Test_selectLowestPriorityNodeGroups_PicksLowest(t *testing.T) {
    // Build three NodeGroups with priorities 0,1,2
    ng0 := &atlassianv1.NodeGroup{ObjectMeta: v1.ObjectMeta{Name: "ng0"}}
    ng1 := &atlassianv1.NodeGroup{ObjectMeta: v1.ObjectMeta{Name: "ng1"}}
    ng2 := &atlassianv1.NodeGroup{ObjectMeta: v1.ObjectMeta{Name: "ng2"}}
    ng0.Spec.Priority = 0
    ng1.Spec.Priority = 1
    ng2.Spec.Priority = 2

    changed := []*ListedNodeGroups{
        buildListed(ng1, "n1"),
        buildListed(ng0, "n0"),
        buildListed(ng2, "n2"),
    }
    ctrl := &controller{metrics: newMetrics()}
    filtered := ctrl.selectLowestPriorityNodeGroups(changed)
    assert.Equal(t, 1, len(filtered))
    assert.Equal(t, "ng0", filtered[0].NodeGroup.Name)
}

func Test_selectLowestPriorityNodeGroups_NegativeDefaultsToZero(t *testing.T) {
    ngNeg := &atlassianv1.NodeGroup{ObjectMeta: v1.ObjectMeta{Name: "neg"}}
    ngPos := &atlassianv1.NodeGroup{ObjectMeta: v1.ObjectMeta{Name: "pos"}}
    ngNeg.Spec.Priority = -10
    ngPos.Spec.Priority = 1

    changed := []*ListedNodeGroups{buildListed(ngNeg, "n0"), buildListed(ngPos, "n1")}
    ctrl := &controller{metrics: newMetrics()}
    filtered := ctrl.selectLowestPriorityNodeGroups(changed)
    assert.Equal(t, 1, len(filtered))
    assert.Equal(t, "neg", filtered[0].NodeGroup.Name)
}

func Test_isLowerPriorityInProgress_Various(t *testing.T) {
    // Build scenario with low (p0) and high (p1)
    scenario := test.BuildTestScenario(test.ScenarioOpts{Keys: []string{"low", "high"}, NodeCount: 1, PodCount: 1}).Flatten()
    var low, high *atlassianv1.NodeGroup
    for _, ng := range scenario.Nodegroups {
        switch ng.Name {
        case "low":
            low = ng
            low.Spec.Priority = 0
        case "high":
            high = ng
            high.Spec.Priority = 1
        }
    }
    // Ensure nodegroups are considered valid (non-zero concurrency)
    low.Spec.CycleSettings.Concurrency = 1
    high.Spec.CycleSettings.Concurrency = 1

    // Build label selectors so ValidateNodeGroup doesn't panic
    selLow, _ := v1.ParseToLabelSelector("select=low")
    selHigh, _ := v1.ParseToLabelSelector("select=high")
    low.Spec.NodeSelector = *selLow
    high.Spec.NodeSelector = *selHigh

    scheme, _ := atlassianv1.SchemeBuilder.Build()
    c := NewFakeClientWithScheme(scheme, low, high)
    // Provide a node lister to pass validation
    nodes := []*corev1.Node{
        {ObjectMeta: v1.ObjectMeta{Name: "n-low", Labels: map[string]string{"select": "low"}}},
        {ObjectMeta: v1.ObjectMeta{Name: "n-high", Labels: map[string]string{"select": "high"}}},
    }
    ctrl := &controller{client: c, nodeLister: test.NewTestNodeWatcher(nodes, test.NodeListerOptions{}), metrics: newMetrics(), Options: Options{Namespace: "kube-system"}}

    // Helper to make CNR for a nodegroup name
    mkCNR := func(ng *atlassianv1.NodeGroup, phase atlassianv1.CycleNodeRequestPhase) atlassianv1.CycleNodeRequest {
        return atlassianv1.CycleNodeRequest{
            ObjectMeta: v1.ObjectMeta{Name: "cnr-" + ng.Name, Namespace: "kube-system"},
            Spec: atlassianv1.CycleNodeRequestSpec{NodeGroupName: ng.Spec.NodeGroupName},
            Status: atlassianv1.CycleNodeRequestStatus{Phase: phase},
        }
    }

    // No in progress
    assert.False(t, ctrl.hasLowerPriorityCNRsInProgress(1, atlassianv1.CycleNodeRequestList{}))

    // Lower exists -> true
    inProg := atlassianv1.CycleNodeRequestList{Items: []atlassianv1.CycleNodeRequest{mkCNR(low, atlassianv1.CycleNodeRequestPending)}}
    assert.True(t, ctrl.hasLowerPriorityCNRsInProgress(1, inProg))

    // Equal priority -> false
    inProgEq := atlassianv1.CycleNodeRequestList{Items: []atlassianv1.CycleNodeRequest{mkCNR(low, atlassianv1.CycleNodeRequestPending)}}
    assert.False(t, ctrl.hasLowerPriorityCNRsInProgress(0, inProgEq))

    // Higher only -> false
    inProgHigh := atlassianv1.CycleNodeRequestList{Items: []atlassianv1.CycleNodeRequest{mkCNR(high, atlassianv1.CycleNodeRequestPending)}}
    assert.False(t, ctrl.hasLowerPriorityCNRsInProgress(1, inProgHigh))

    // Negative priority on lower treated as 0: with min=1 -> true, with min=0 -> false
    low.Spec.Priority = -3
    _ = c.Update(context.TODO(), low)
    assert.True(t, ctrl.hasLowerPriorityCNRsInProgress(1, inProg))
    assert.False(t, ctrl.hasLowerPriorityCNRsInProgress(0, inProg))
}
