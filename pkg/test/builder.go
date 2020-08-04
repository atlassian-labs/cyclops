package test

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
)

// NodeOpts minimal options for configuring a node object in testing
type NodeOpts struct {
	Name       string
	CPU        int64
	Mem        int64
	LabelKey   string
	LabelValue string
	Creation   time.Time
	Tainted    bool
}

// BuildFakeClient creates a fake client
func BuildFakeClient(nodes []*corev1.Node, pods []*corev1.Pod) (*fake.Clientset, <-chan string) {
	fakeClient := &fake.Clientset{}
	updateChan := make(chan string, 2*(len(nodes)+len(pods)))
	// nodes
	fakeClient.Fake.AddReactor("get", "nodes", func(action core.Action) (bool, runtime.Object, error) {
		getAction := action.(core.GetAction)
		for _, node := range nodes {
			if node.Name == getAction.GetName() {
				return true, node, nil
			}
		}
		return true, nil, fmt.Errorf("No node named: %v", getAction.GetName())
	})
	fakeClient.Fake.AddReactor("update", "nodes", func(action core.Action) (bool, runtime.Object, error) {
		updateAction := action.(core.UpdateAction)
		node := updateAction.GetObject().(*corev1.Node)
		for _, n := range nodes {
			if node.Name == n.Name {
				updateChan <- node.Name
				return true, node, nil
			}
		}
		return false, nil, fmt.Errorf("No node named: %v", node.Name)
	})
	fakeClient.Fake.AddReactor("list", "nodes", func(action core.Action) (bool, runtime.Object, error) {
		nodesCopy := make([]corev1.Node, 0, len(nodes))
		for _, n := range nodes {
			nodesCopy = append(nodesCopy, *n)
		}
		return true, &corev1.NodeList{Items: nodesCopy}, nil
	})

	// pods
	fakeClient.Fake.AddReactor("get", "pods", func(action core.Action) (bool, runtime.Object, error) {
		getAction := action.(core.GetAction)
		for _, pod := range pods {
			if pod.Name == getAction.GetName() && pod.Namespace == getAction.GetNamespace() {
				return true, pod, nil
			}
		}
		return true, nil, fmt.Errorf("No pod named: %v", getAction.GetName())
	})
	fakeClient.Fake.AddReactor("update", "pods", func(action core.Action) (bool, runtime.Object, error) {
		updateAction := action.(core.UpdateAction)
		pod := updateAction.GetObject().(*corev1.Pod)
		for _, p := range pods {
			if pod.Name == p.Name {
				updateChan <- pod.Name
				return true, pod, nil
			}
		}
		return false, nil, fmt.Errorf("No pod named: %v", pod.Name)
	})
	fakeClient.Fake.AddReactor("list", "pods", func(action core.Action) (bool, runtime.Object, error) {
		podsCopy := make([]corev1.Pod, 0, len(pods))
		for _, p := range pods {
			podsCopy = append(podsCopy, *p)
		}
		return true, &corev1.PodList{Items: podsCopy}, nil
	})
	return fakeClient, updateChan
}

// NameFromChan returns a name from a channel update
// fails if timeout
func NameFromChan(c <-chan string, timeout time.Duration) string {
	select {
	case val := <-c:
		return val
	case <-time.After(timeout):
		return "Nothing returned"
	}
}

// BuildTestNode creates a node with specified capacity.
func BuildTestNode(opts NodeOpts) *corev1.Node {

	var taints []corev1.Taint
	if opts.Tainted {
		taints = append(taints, corev1.Taint{
			Key:    "atlassian.com/cyclops",
			Value:  fmt.Sprint(time.Now().Unix()),
			Effect: corev1.TaintEffectNoSchedule,
		})
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:     opts.Name,
			SelfLink: fmt.Sprintf("/api/v1/nodes/%s", opts.Name),
			Labels: map[string]string{
				opts.LabelKey: opts.LabelValue,
			},
			CreationTimestamp: metav1.NewTime(opts.Creation),
		},
		Spec: corev1.NodeSpec{
			ProviderID: opts.Name,
			Taints:     taints,
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourcePods: *resource.NewQuantity(100, resource.DecimalSI),
			},
		},
	}

	if opts.CPU >= 0 {
		node.Status.Capacity[corev1.ResourceCPU] = *resource.NewMilliQuantity(opts.CPU, resource.DecimalSI)
	}
	if opts.Mem >= 0 {
		node.Status.Capacity[corev1.ResourceMemory] = *resource.NewQuantity(opts.Mem, resource.DecimalSI)
	}

	node.Status.Allocatable = corev1.ResourceList{}
	for k, v := range node.Status.Capacity {
		node.Status.Allocatable[k] = v
	}

	return node
}

// BuildTestNodes creates multiple nodes with the same options
func BuildTestNodes(amount int, opts NodeOpts) []*corev1.Node {
	var nodes []*corev1.Node
	for i := 0; i < amount; i++ {
		opts.Name = uuid.New().String()
		nodes = append(nodes, BuildTestNode(opts))
	}
	return nodes
}

// PodOpts are options for a pod
type PodOpts struct {
	Name              string
	Namespace         string
	CPU               []int64
	Mem               []int64
	NodeSelectorKey   string
	NodeSelectorValue string
	Owner             string
	OwnerName         string
	NodeAffinityKey   string
	NodeAffinityValue string
	NodeAffinityOp    corev1.NodeSelectorOperator
	NodeName          string
}

// BuildTestPod builds a pod for testing
func BuildTestPod(opts PodOpts) *corev1.Pod {
	containers := make([]corev1.Container, 0, len(opts.CPU))
	for range opts.CPU {
		containers = append(containers, corev1.Container{
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{},
			},
		})
	}

	var owners []metav1.OwnerReference
	if len(opts.Owner) > 0 {
		owners = append(owners, metav1.OwnerReference{
			Kind: opts.Owner,
			Name: opts.OwnerName,
		})
	}

	var nodeSelector map[string]string
	if len(opts.NodeSelectorKey) > 0 || len(opts.NodeSelectorValue) > 0 {
		nodeSelector = map[string]string{
			opts.NodeSelectorKey: opts.NodeSelectorValue,
		}
	}

	var affinity *corev1.Affinity
	if len(opts.NodeAffinityKey) > 0 || len(opts.NodeAffinityValue) > 0 {
		if opts.NodeAffinityOp == "" {
			opts.NodeAffinityOp = corev1.NodeSelectorOpIn
		}
		affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key: opts.NodeAffinityKey,
									Values: []string{
										opts.NodeAffinityValue,
									},
									Operator: opts.NodeAffinityOp,
								},
							},
						},
					},
				},
			},
		}
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       opts.Namespace,
			Name:            opts.Name,
			SelfLink:        fmt.Sprintf("/api/v1/namespaces/%s/pods/%s", opts.Namespace, opts.Name),
			OwnerReferences: owners,
		},
		Spec: corev1.PodSpec{
			Containers:   containers,
			NodeSelector: nodeSelector,
			Affinity:     affinity,
		},
	}

	if len(opts.NodeName) > 0 {
		pod.Spec.NodeName = opts.NodeName
	}

	for i := range containers {
		if opts.CPU[i] >= 0 {
			pod.Spec.Containers[i].Resources.Requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(opts.CPU[i], resource.DecimalSI)
		}
		if opts.Mem[i] >= 0 {
			pod.Spec.Containers[i].Resources.Requests[corev1.ResourceMemory] = *resource.NewQuantity(opts.Mem[i], resource.DecimalSI)
		}
	}

	return pod
}

// BuildTestPods creates multiple pods with the same options
func BuildTestPods(amount int, opts PodOpts) []*corev1.Pod {
	var pods []*corev1.Pod
	for i := 0; i < amount; i++ {
		opts.Name = fmt.Sprintf("p%d", i)
		pods = append(pods, BuildTestPod(opts))
	}
	return pods
}

// Scenario is a scenario of nodegroups and related components as a map to list
type Scenario struct {
	Nodegroups          map[string]*atlassianv1.NodeGroup
	Nodes               map[string][]*corev1.Node
	Pods                map[string][]*corev1.Pod
	Daemonsets          map[string][]*appsv1.DaemonSet
	ControllerRevisions map[string][]*appsv1.ControllerRevision
}

func (s *Scenario) Flatten() *FlatScenario {
	var keys []string
	for key := range s.Nodegroups {
		keys = append(keys, key)
	}
	return FlattenScenario(s, keys...)
}

// FlatScenario is a scenario of nodegroups and related components flatted into a single list
type FlatScenario struct {
	Nodegroups          []*atlassianv1.NodeGroup
	Nodes               []*corev1.Node
	Pods                []*corev1.Pod
	Daemonsets          []*appsv1.DaemonSet
	ControllerRevisions []*appsv1.ControllerRevision
}

// ScenarioOpts for creating Scenario
type ScenarioOpts struct {
	Keys      []string
	NodeCount int
	PodCount  int

	PodsUpToDate map[string]bool
}

// FlattenScenario turns a Scenario into a FlatScenario
func FlattenScenario(scenario *Scenario, includeKeys ...string) *FlatScenario {
	flattened := &FlatScenario{}
	for key := range scenario.Nodegroups {
		for _, in := range includeKeys {
			if key == in {
				flattened.Nodegroups = append(flattened.Nodegroups, scenario.Nodegroups[key])
				flattened.Nodes = append(flattened.Nodes, scenario.Nodes[key]...)
				flattened.Pods = append(flattened.Pods, scenario.Pods[key]...)
				flattened.Daemonsets = append(flattened.Daemonsets, scenario.Daemonsets[key]...)
				flattened.ControllerRevisions = append(flattened.ControllerRevisions, scenario.ControllerRevisions[key]...)
				break
			}
		}
	}
	return flattened
}

func (f *FlatScenario) NodeGroupList() atlassianv1.NodeGroupList {
	var ngList atlassianv1.NodeGroupList
	for i := range f.Nodegroups {
		ngList.Items = append(ngList.Items, *f.Nodegroups[i])
	}
	return ngList
}

// BuildTestScenario from a ScenarioOpts
func BuildTestScenario(opts ScenarioOpts) *Scenario {
	const controllerRevisionLabel = "controller-revision-hash"
	oldhash := "oldhash"
	latesthash := "latesthash"

	nodegroups := make(map[string]*atlassianv1.NodeGroup)
	nodes := make(map[string][]*corev1.Node)
	pods := make(map[string][]*corev1.Pod)
	daemonsets := make(map[string][]*appsv1.DaemonSet)
	controllerRevisions := make(map[string][]*appsv1.ControllerRevision)

	for _, key := range opts.Keys {
		selectorMeta, _ := metav1.ParseToLabelSelector("select=" + key)
		nodegroup := &atlassianv1.NodeGroup{}
		nodegroup.Name = key
		nodegroup.Spec.NodeSelector = *selectorMeta
		nodegroup.Spec.NodeGroupName = fmt.Sprint("nodegroup-", key)
		nodegroups[key] = nodegroup

		nodes[key] = BuildTestNodes(opts.NodeCount, NodeOpts{
			LabelKey:   "select",
			LabelValue: key,
		})

		for i := 0; i < opts.PodCount; i++ {
			pods[key] = append(pods[key], BuildTestPod(PodOpts{
				Name:              fmt.Sprint("pod-", key, "-", i),
				Namespace:         "kube-system",
				NodeSelectorKey:   "select",
				NodeSelectorValue: key,
				Owner:             "DaemonSet",
				OwnerName:         fmt.Sprint("ds-", key),
				NodeName:          nodes[key][i%opts.NodeCount].Name,
			}))
			hash := oldhash
			if opts.PodsUpToDate[key] {
				hash = latesthash
			}
			pods[key][i].Labels = map[string]string{controllerRevisionLabel: hash}
			pods[key][i].Status.Phase = corev1.PodRunning
		}

		daemonsets[key] = append(daemonsets[key], &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprint("ds-", key),
				Namespace: "kube-system",
			},
			Spec: appsv1.DaemonSetSpec{
				UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
					Type: appsv1.OnDeleteDaemonSetStrategyType,
				},
				Selector: selectorMeta,
			},
		})

		controllerRevisions[key] = append(controllerRevisions[key], &appsv1.ControllerRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprint("cr-old-", key),
				Namespace: "kube-system",
				Labels: map[string]string{
					"select":                key,
					controllerRevisionLabel: oldhash,
				},
			},
			Revision: 1,
		})

		controllerRevisions[key] = append(controllerRevisions[key], &appsv1.ControllerRevision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprint("cr-latest-", key),
				Namespace: "kube-system",
				Labels: map[string]string{
					"select":                key,
					controllerRevisionLabel: latesthash,
				},
			},
			Revision: 2,
		})
	}

	return &Scenario{
		Nodegroups:          nodegroups,
		Nodes:               nodes,
		Pods:                pods,
		Daemonsets:          daemonsets,
		ControllerRevisions: controllerRevisions,
	}
}
