package mock

import (
	"fmt"
	"net/http"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider/aws"
	"github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/atlassian-labs/cyclops/pkg/controller/cyclenoderequest/transitioner"

	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"

	fakerawclient "k8s.io/client-go/kubernetes/fake"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"

	runtime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Node struct {
	Name       string
	LabelKey   string
	LabelValue string
	Creation   time.Time
	Tainted    bool
	Nodegroup  string
	InstanceID string

	NodeReady corev1.ConditionStatus

	CPU int64
	Mem int64

	state      string
	providerID string
}

type MockClient struct {
	*transitioner.CycleNodeRequestTransitioner

	Cnr     *v1.CycleNodeRequest
	rm      *controller.ResourceManager
	options transitioner.Options

	cloudProviderInstances []*Node
	kubeNodes              []*Node

	// AWS
	Autoscaling autoscalingiface.AutoScalingAPI
	Ec2         ec2iface.EC2API

	// KUBE
	K8sClient client.Client
	RawClient kubernetes.Interface
}

func NewMockTransitioner(opts ...Option) MockClient {
	t := MockClient{}

	for _, opt := range opts {
		opt(&t)
	}

	for _, node := range t.kubeNodes {
		generateProviderID(node)
	}

	for _, node := range t.cloudProviderInstances {
		generateProviderID(node)
		node.state = "running"
	}

	runtimeNodes, clientNodes := generateKubeNodes(t.kubeNodes)

	scheme := runtime.NewScheme()
	utilruntime.Must(addCustomSchemes(scheme))

	kubeObjects := clientNodes
	kubeObjects = append(kubeObjects, t.Cnr)

	t.K8sClient = fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(kubeObjects...).Build()
	t.RawClient = fakerawclient.NewSimpleClientset(runtimeNodes...)

	t.Autoscaling = &MockAutoscaling{
		cloudProviderInstances: &t.cloudProviderInstances,
	}

	t.Ec2 = &MockEc2{
		cloudProviderInstances: &t.cloudProviderInstances,
	}

	t.rm = &controller.ResourceManager{
		Client:        t.K8sClient,
		RawClient:     t.RawClient,
		HttpClient:    http.DefaultClient,
		CloudProvider: aws.NewMockCloudProvider(t.Autoscaling, t.Ec2),
	}

	t.CycleNodeRequestTransitioner = transitioner.NewCycleNodeRequestTransitioner(t.Cnr, t.rm, t.options)
	return t
}

func addCustomSchemes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(v1.SchemeGroupVersion, &v1.CycleNodeRequest{})
	scheme.AddKnownTypes(v1.SchemeGroupVersion, &v1.CycleNodeRequestList{})
	scheme.AddKnownTypes(v1.SchemeGroupVersion, &v1.CycleNodeStatus{})
	scheme.AddKnownTypes(v1.SchemeGroupVersion, &v1.CycleNodeStatusList{})
	scheme.AddKnownTypes(v1.SchemeGroupVersion, &v1.NodeGroup{})
	scheme.AddKnownTypes(v1.SchemeGroupVersion, &v1.NodeGroupList{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Node{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.NodeList{})
	return nil
}

// func generateCloudProviderInstances(nodes []*Node) ([]runtime.Object, []client.Object) {
// 	runtimeNodes := make([]runtime.Object, 0)
// 	clientNodes := make([]client.Object, 0)

// 	for _, node := range nodes {
// 		kubeNode := buildKubeNode(node)
// 		runtimeNodes = append(runtimeNodes, kubeNode)
// 		clientNodes = append(clientNodes, kubeNode)
// 	}

// 	return runtimeNodes, clientNodes
// }

func generateKubeNodes(nodes []*Node) ([]runtime.Object, []client.Object) {
	runtimeNodes := make([]runtime.Object, 0)
	clientNodes := make([]client.Object, 0)

	for _, node := range nodes {
		kubeNode := buildKubeNode(node)
		runtimeNodes = append(runtimeNodes, kubeNode)
		clientNodes = append(clientNodes, kubeNode)
	}

	return runtimeNodes, clientNodes
}

func buildKubeNode(node *Node) *corev1.Node {
	var taints []corev1.Taint

	if node.Tainted {
		taints = append(taints, corev1.Taint{
			Key:    "atlassian.com/cyclops",
			Value:  fmt.Sprint(time.Now().Unix()),
			Effect: corev1.TaintEffectNoSchedule,
		})
	}

	kubeNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:     node.Name,
			SelfLink: fmt.Sprintf("/api/v1/nodes/%s", node.Name),
			Labels: map[string]string{
				node.LabelKey: node.LabelValue,
			},
			CreationTimestamp: metav1.NewTime(node.Creation),
		},
		Spec: corev1.NodeSpec{
			ProviderID: node.providerID,
			Taints:     taints,
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourcePods: *resource.NewQuantity(100, resource.DecimalSI),
			},
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: node.NodeReady,
				},
			},
		},
	}

	if node.CPU >= 0 {
		kubeNode.Status.Capacity[corev1.ResourceCPU] = *resource.NewMilliQuantity(node.CPU, resource.DecimalSI)
	}

	if node.Mem >= 0 {
		kubeNode.Status.Capacity[corev1.ResourceMemory] = *resource.NewQuantity(node.Mem, resource.DecimalSI)
	}

	kubeNode.Status.Allocatable = corev1.ResourceList{}

	for k, v := range kubeNode.Status.Capacity {
		kubeNode.Status.Allocatable[k] = v
	}

	return kubeNode
}
