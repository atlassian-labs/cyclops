package transitioner

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
)

func (t *CycleNodeStatusTransitioner) removeLabelsFromPods() (finished bool, err error) {
	if len(t.cycleNodeStatus.Spec.CycleSettings.LabelsToRemove) == 0 {
		return true, nil
	}

	// List all pods on the node
	pods, err := t.rm.GetPodsOnNode(t.cycleNodeStatus.Status.CurrentNode.Name)
	if err != nil {
		return false, err
	}

	// Remove any matching labels from the pod
	labelsRemoved := 0
	for _, pod := range pods {
		var operations []k8s.Patch
		// Check to see if the pod has the label
		for _, label := range t.cycleNodeStatus.Spec.CycleSettings.LabelsToRemove {
			if _, ok := pod.Labels[label]; ok {
				operations = append(operations, k8s.Patch{
					Op:   "remove",
					Path: fmt.Sprintf("/metadata/labels/%s", label),
				})
			}
		}

		// Remove the labels by patching the pod
		if len(operations) > 0 {
			labelsRemoved += len(operations)
			for _, operation := range operations {
				msg := fmt.Sprintf("Removing label %v from pod %v/%v", operation.Path, pod.Namespace, pod.Name)
				t.rm.Logger.Info(msg)
				t.rm.LogEvent(t.cycleNodeStatus, "RemovingLabel", msg)
			}
			if err := k8s.PatchPod(pod.Name, pod.Namespace, operations, t.rm.RawClient); err != nil {
				return finished, err
			}
		}
	}

	// We track the amount of labels removed to ensure we have
	// removed all of the labels before progressing
	return labelsRemoved == 0, nil
}

// podsFinished returns true if all relevant pods on the node are finished.
func (t *CycleNodeStatusTransitioner) podsFinished() (bool, error) {
	// Get drainable pods
	drainablePods, err := t.rm.GetDrainablePodsOnNode(t.cycleNodeStatus.Status.CurrentNode.Name)
	if err != nil {
		return false, err
	}

	waitingOnPods, err := getRunningPods(
		drainablePods,
		t.cycleNodeStatus.Spec.CycleSettings.IgnoreNamespaces,
		t.cycleNodeStatus.Spec.CycleSettings.IgnorePodsLabels,
	)
	if err != nil {
		return false, err
	}
	t.rm.Logger.Info("Waiting for pods on node", "node", t.cycleNodeStatus.Spec.NodeName, "pods", waitingOnPods)
	return len(waitingOnPods) == 0, nil
}

// getRunningPods uses the CycleNodeRequestWaitRules provided to filter down the list of runningPods
// to those which are not ignored by the ignoreNamespaces and ignorePodsLabels options.
func getRunningPods(runningPods []corev1.Pod, ignoreNamespaces []string, ignorePodsLabels map[string][]string) ([]corev1.Pod, error) {
	filteredRunningPods := make([]corev1.Pod, 0)
	for _, pod := range runningPods {
		ignorePod := false
		// Check namespace rules
		for _, namespace := range ignoreNamespaces {
			if pod.Namespace == namespace {
				ignorePod = true
				break
			}
		}
		// Check all the label rules
		for ignoreLabelName, ignoreLabelValues := range ignorePodsLabels {
			// If the pod has this label
			if podLabelValue, ok := pod.Labels[ignoreLabelName]; ok {
				// And the value for the label is in the list values to ignore
				for _, ignoreLabelValue := range ignoreLabelValues {
					if podLabelValue == ignoreLabelValue {
						ignorePod = true
						break
					}
				}
			}
			// Don't keep checking labels if we're already ignoring the pod
			if ignorePod {
				break
			}
		}
		// If pod has not been ignored, remember it
		if !ignorePod {
			filteredRunningPods = append(filteredRunningPods, pod)
		}
	}

	return filteredRunningPods, nil
}
