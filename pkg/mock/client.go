package mock

import (
	"fmt"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider/aws"
	fakeaws "github.com/atlassian-labs/cyclops/pkg/cloudprovider/aws/fake"

	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"

	fakerawclient "k8s.io/client-go/kubernetes/fake"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Node struct {
	Name       string
	Creation   time.Time
	Tainted    bool
	Nodegroup  string
	InstanceID string

	LabelKey        string
	LabelValue      string
	AnnotationKey   string
	AnnotationValue string

	NodeReady corev1.ConditionStatus

	CPU int64
	Mem int64

	CloudProviderState string
	ProviderID         string
}

type Client struct {
	// AWS
	Autoscaling autoscalingiface.AutoScalingAPI
	Ec2         ec2iface.EC2API

	cloudprovider.CloudProvider

	// KUBE
	K8sClient client.Client
	RawClient kubernetes.Interface
}

func NewClient(kubeNodes []*Node, cloudProviderNodes []*Node, extraKubeObjects ...client.Object) *Client {
	t := &Client{}

	// Add the providerID to all nodes
	for _, node := range kubeNodes {
		node.ProviderID = fakeaws.GenerateProviderID(node.InstanceID)
	}

	for _, node := range cloudProviderNodes {
		node.ProviderID = fakeaws.GenerateProviderID(node.InstanceID)
	}

	runtimeNodes, clientNodes := generateKubeNodes(kubeNodes)

	scheme := runtime.NewScheme()
	utilruntime.Must(addCustomSchemes(scheme))

	kubeObjects := clientNodes
	kubeObjects = append(kubeObjects, extraKubeObjects...)

	t.K8sClient = fakeclient.NewClientBuilder().WithScheme(scheme).WithObjects(kubeObjects...).Build()
	t.RawClient = fakerawclient.NewSimpleClientset(runtimeNodes...)

	cloudProviderInstances := generateFakeInstances(cloudProviderNodes)

	autoscalingiface := &fakeaws.Autoscaling{
		Instances: cloudProviderInstances,
	}

	ec2iface := &fakeaws.Ec2{
		Instances: cloudProviderInstances,
	}

	t.Autoscaling = autoscalingiface
	t.Ec2 = ec2iface
	t.CloudProvider = aws.NewGenericCloudProvider(autoscalingiface, ec2iface)

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

func generateFakeInstances(nodes []*Node) map[string]*fakeaws.Instance {
	var instances = make(map[string]*fakeaws.Instance, 0)

	for _, node := range nodes {
		instances[node.InstanceID] = &fakeaws.Instance{
			InstanceID:           node.InstanceID,
			AutoscalingGroupName: node.Nodegroup,
			State:                node.CloudProviderState,
		}
	}

	return instances
}

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
			Annotations: map[string]string{
				node.AnnotationKey: node.AnnotationValue,
			},
			CreationTimestamp: metav1.NewTime(node.Creation),
		},
		Spec: corev1.NodeSpec{
			ProviderID: node.ProviderID,
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
