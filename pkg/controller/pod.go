package controller

import (
	"context"

	"github.com/atlassian-labs/cyclops/pkg/k8s"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetPodsOnNode gets a list of the pods running on the given node, optionally filtered by the given label selector.
func (rm *ResourceManager) GetPodsOnNode(nodeName string) (pods []v1.Pod, err error) {
	podList := &v1.PodList{}
	listOptions := &client.ListOptions{
		Namespace: "",
		FieldSelector: fields.SelectorFromSet(map[string]string{
			"spec.nodeName": nodeName,
		}),
	}
	if err := rm.Client.List(context.TODO(), podList, listOptions); err != nil {
		return pods, err
	}
	return podList.Items, nil
}

func getUndisruptablePods(pods []v1.Pod) []v1.Pod {
	filteredPods := make([]v1.Pod, 0)

	for _, pod := range pods {
		if k8s.PodCannotBeDisrupted(&pod) && pod.Status.Phase == v1.PodRunning {
			filteredPods = append(filteredPods, pod)
		}
	}

	return filteredPods
}

// GetUndisruptablePods gets a list of pods on a named node that cannot evicted or deleted from the node.
func (rm *ResourceManager) GetUndisruptablePods(nodeName string) (pods []v1.Pod, err error) {
	allPods, err := rm.GetPodsOnNode(nodeName)
	if err != nil {
		return pods, err
	}

	return getUndisruptablePods(allPods), nil
}

// GetDrainablePodsOnNode gets a list of pods on a named node that we can evict or delete from the node.
func (rm *ResourceManager) GetDrainablePodsOnNode(nodeName string) (pods []v1.Pod, err error) {
	allPods, err := rm.GetPodsOnNode(nodeName)
	if err != nil {
		return pods, err
	}

	for _, pod := range allPods {
		if !k8s.PodIsDaemonSet(&pod) && !k8s.PodIsStatic(&pod) && pod.Status.Phase == v1.PodRunning {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}
