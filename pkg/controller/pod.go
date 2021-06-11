package controller

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
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
