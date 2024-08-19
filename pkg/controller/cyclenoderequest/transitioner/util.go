package transitioner

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
)

// transitionToHealing transitions the current cycleNodeRequest to healing which will always transiting to failed
func (t *CycleNodeRequestTransitioner) transitionToHealing(err error) (reconcile.Result, error) {
	return t.transitionToUnsuccessful(v1.CycleNodeRequestHealing, err)
}

// transitionToFailed transitions the current cycleNodeRequest to failed
func (t *CycleNodeRequestTransitioner) transitionToFailed(err error) (reconcile.Result, error) {
	// Block transitioning to Failed twice in a row
	if t.cycleNodeRequest.Status.Phase == v1.CycleNodeRequestFailed {
		return reconcile.Result{}, nil
	}

	return t.transitionToUnsuccessful(v1.CycleNodeRequestFailed, err)
}

// transitionToSuccessful transitions the current cycleNodeRequest to successful
func (t *CycleNodeRequestTransitioner) transitionToSuccessful() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeRequest, "Successful", "Successfully cycled nodes")
	t.cycleNodeRequest.Status.Phase = v1.CycleNodeRequestSuccessful

	// Notify that the cycling has succeeded
	if t.rm.Notifier != nil {
		if err := t.rm.Notifier.PhaseTransitioned(t.cycleNodeRequest); err != nil {
			t.rm.Logger.Error(err, "Unable to post message to messaging provider", "phase", t.cycleNodeRequest.Status.Phase)
		}
	}

	return reconcile.Result{}, t.rm.UpdateObject(t.cycleNodeRequest)
}

// transitionObject transitions the current cycleNodeRequest to the specified phase
func (t *CycleNodeRequestTransitioner) transitionObject(desiredPhase v1.CycleNodeRequestPhase) (reconcile.Result, error) {
	currentPhase := t.cycleNodeRequest.Status.Phase
	t.cycleNodeRequest.Status.Phase = desiredPhase
	if err := t.rm.UpdateObject(t.cycleNodeRequest); err != nil {
		return reconcile.Result{}, err
	}

	// Notify that the cycling has transitioned to a new phase
	if t.rm.Notifier != nil && currentPhase != desiredPhase {
		if err := t.rm.Notifier.PhaseTransitioned(t.cycleNodeRequest); err != nil {
			t.rm.Logger.Error(err, "Unable to post message to messaging provider", "phase", t.cycleNodeRequest.Status.Phase)
		}
	}

	return reconcile.Result{
		Requeue:      true,
		RequeueAfter: transitionDuration,
	}, nil
}

// equilibriumWaitTimedOut returns true if we have exceeded the wait time for the node group and the kube nodes to
// come into equilibrium.
func (t *CycleNodeRequestTransitioner) equilibriumWaitTimedOut() (bool, error) {
	// If the timer isn't initialised, initialise it and save it to the object
	if t.cycleNodeRequest.Status.EquilibriumWaitStarted.IsZero() {
		t.rm.Logger.Info("started equilibrium wait")
		currentTime := metav1.Now()
		t.cycleNodeRequest.Status.EquilibriumWaitStarted = &currentTime
		if err := t.rm.UpdateObject(t.cycleNodeRequest); err != nil {
			return false, err
		}
	}
	return time.Now().After(t.cycleNodeRequest.Status.EquilibriumWaitStarted.Time.Add(nodeEquilibriumWaitLimit)), nil
}

// reapChildren reaps CycleNodeStatus children. It returns the state that should be
// transitioned into based on what the children are doing. If a child is not in
// the Successful or Failed phase then it will not be reaped.
func (t *CycleNodeRequestTransitioner) reapChildren() (v1.CycleNodeRequestPhase, error) {
	nextPhase := t.cycleNodeRequest.Status.Phase

	// List the cycleNodeStatus objects in the cluster
	cycleNodeStatusList := &v1.CycleNodeStatusList{}

	labelSelector, err := labels.Parse("name=" + t.cycleNodeRequest.Name)
	if err != nil {
		return nextPhase, err
	}

	listOptions := client.ListOptions{
		Namespace:     t.cycleNodeRequest.Namespace,
		LabelSelector: labelSelector,
	}

	err = t.rm.Client.List(context.TODO(), cycleNodeStatusList, &listOptions)
	if err != nil {
		return nextPhase, err
	}

	// Check all of the children - if any are failed, the whole CycleNodeRequest fails
	inProgressCount := 0
	for _, cycleNodeStatus := range cycleNodeStatusList.Items {
		switch cycleNodeStatus.Status.Phase {
		case v1.CycleNodeStatusFailed:
			nextPhase = v1.CycleNodeRequestHealing
			t.rm.LogWarningEvent(t.cycleNodeRequest, "ReapChildren", "Failed to cycle node: %v, reason: %v", cycleNodeStatus.Spec.NodeName, cycleNodeStatus.Status.Message)
			t.rm.Logger.Info("Child has failed", "nodeName", cycleNodeStatus.Name, "status", cycleNodeStatus.Status.Phase, "message", cycleNodeStatus.Status.Message)
			fallthrough
		case v1.CycleNodeStatusSuccessful:
			// Delete the Failed and Successful children alike
			err := t.rm.Client.Delete(context.TODO(), &cycleNodeStatus)
			t.rm.Logger.Info("Reaped child", "nodeName", cycleNodeStatus.Name, "status", cycleNodeStatus.Status.Phase)
			if err != nil {
				return nextPhase, err
			}
		default:
			inProgressCount++
		}
	}

	// Update the count of our active children so we can use this to determine how many more nodes
	// to schedule at a time.
	if int64(inProgressCount) != t.cycleNodeRequest.Status.ActiveChildren {
		t.cycleNodeRequest.Status.ActiveChildren = int64(inProgressCount)
	}

	// Stay in the WaitingTermination phase if there are no nodes left to pick up for cycling but still nodes being cycled
	if len(t.cycleNodeRequest.Status.NodesAvailable) == 0 && t.cycleNodeRequest.Status.ActiveChildren > 0 {
		return nextPhase, nil
	}

	// If we've finished most of our children, go back to Initialised to add some more nodes
	// It is assumed that nodes selected for cycling will take roughly the same time to finish
	// Bringing up multiple nodes together will speed up the whole process as well as spread out pods properly across the new nodes
	// If the next phase should be failed, skip this since transitioning back to initialised would be flip-flopping behaviour
	if nextPhase != v1.CycleNodeRequestHealing && t.cycleNodeRequest.Status.ActiveChildren <= t.cycleNodeRequest.Spec.CycleSettings.Concurrency/2 {
		t.rm.Logger.Info("Transition back to Initialised to grab more child nodes", "ActiveChildren", t.cycleNodeRequest.Status.ActiveChildren, "Concurrency", t.cycleNodeRequest.Spec.CycleSettings.Concurrency)
		nextPhase = v1.CycleNodeRequestInitialised
	}
	return nextPhase, nil
}

// finalReapChildren handles reaping of children where instead of going back to Initialised,
// we need to end the cycle for this CycleNodeRequest.
func (t *CycleNodeRequestTransitioner) finalReapChildren() (shouldRequeue bool, err error) {
	t.cycleNodeRequest.Status.Phase, err = t.reapChildren()
	if err != nil {
		return true, err
	}

	switch t.cycleNodeRequest.Status.Phase {
	case v1.CycleNodeRequestInitialised, v1.CycleNodeRequestFailed:
		if t.cycleNodeRequest.Status.ActiveChildren == 0 {
			// No more work to be done, stop processing this request
			return false, nil
		}
		fallthrough
	default:
		if err := t.rm.UpdateObject(t.cycleNodeRequest); err != nil {
			return true, err
		}
		// Still waiting on some children, keep reaping
		return true, nil
	}
}

// removeOldChildrenFromCluster removes any leftover children from a previous CycleNodeRequest with the same
// name.
func (t *CycleNodeRequestTransitioner) removeOldChildrenFromCluster() error {
	cycleNodeStatusList := &v1.CycleNodeStatusList{}

	labelSelector, err := labels.Parse("name=" + t.cycleNodeRequest.Name)
	if err != nil {
		return err
	}

	listOptions := client.ListOptions{
		Namespace:     t.cycleNodeRequest.Namespace,
		LabelSelector: labelSelector,
	}

	err = t.rm.Client.List(context.TODO(), cycleNodeStatusList, &listOptions)
	if err != nil {
		return err
	}

	for _, cns := range cycleNodeStatusList.Items {
		err := t.rm.Client.Delete(context.TODO(), &cns)
		if err != nil {
			return err
		}
		t.rm.Logger.Info("Removed old child for CycleNodRequest", "cycleNodeRequest.Name", t.cycleNodeRequest.Name, "cycleNodeStatus.Name", cns.Name)
	}
	return nil
}

// makeCycleNodeStatusForNode creates a CycleNodeStatus object based on this CycleNodeRequest object, for the
// given node name.
func (t *CycleNodeRequestTransitioner) makeCycleNodeStatusForNode(nodeName string) *v1.CycleNodeStatus {
	nodeStatus := &v1.CycleNodeStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", t.cycleNodeRequest.Name, nodeName),
			Namespace: t.cycleNodeRequest.Namespace,
			Labels: map[string]string{
				"name": t.cycleNodeRequest.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(t.cycleNodeRequest, schema.GroupVersionKind{
					Group:   t.cycleNodeRequest.GroupVersionKind().Group,
					Version: t.cycleNodeRequest.GroupVersionKind().Version,
					Kind:    t.cycleNodeRequest.GroupVersionKind().Kind,
				}),
			},
		},
		Spec: v1.CycleNodeStatusSpec{
			NodeName:      nodeName,
			CycleSettings: t.cycleNodeRequest.Spec.CycleSettings,
		},
	}
	return nodeStatus
}

// Checks if the phase should be transitioned to either WaitingTermination or Successful based on the nodes left to cycle and in progress
func (t *CycleNodeRequestTransitioner) checkIfTransitioning(numNodesToCycle, numNodesInProgress int) (bool, reconcile.Result, error) {
	// If no nodes are left to cycle
	if numNodesToCycle == 0 {
		// If there are still cycle operations in progress, then transition to the WaitingTermination phase
		// to wait for them. Transitioning straight to Successful would be bad if one of them were to fail.
		if numNodesInProgress > 0 {
			t.rm.Logger.Info("All remaining nodes in progress, waiting termination of final CycleNodeStatuses")

			transition, err := t.transitionObject(v1.CycleNodeRequestWaitingTermination)
			return true, transition, err
		}

		// otherwise, we have finished everything, so transition to Successful
		transition, err := t.transitionToSuccessful()
		return true, transition, err
	}

	return false, reconcile.Result{}, nil
}

// findOffendingNodes finds the offending nodes information which cause number of nodes mismatch between
// cloud provider node group and nodes inside kubernetes cluster using label selector
func findOffendingNodes(kubeNodes map[string]corev1.Node, cloudProviderNodes map[string]cloudprovider.Instance) (map[string]corev1.Node, map[string]cloudprovider.Instance) {
	var nodesNotInCPNodeGroup = make(map[string]corev1.Node)
	var nodesNotInKube = make(map[string]cloudprovider.Instance)

	for _, kubeNode := range kubeNodes {
		if _, ok := cloudProviderNodes[kubeNode.Spec.ProviderID]; !ok {
			nodesNotInCPNodeGroup[kubeNode.Spec.ProviderID] = kubeNode
		}
	}

	for providerID, cpNode := range cloudProviderNodes {
		if _, ok := kubeNodes[providerID]; !ok {
			nodesNotInKube[providerID] = cpNode
		}
	}

	return nodesNotInCPNodeGroup, nodesNotInKube
}

// transitionToUnsuccessful transitions the current cycleNodeRequest to healing/failed
func (t *CycleNodeRequestTransitioner) transitionToUnsuccessful(phase v1.CycleNodeRequestPhase, err error) (reconcile.Result, error) {
	t.cycleNodeRequest.Status.Phase = phase
	// don't try to append message if it's nil
	if err != nil {
		if t.cycleNodeRequest.Status.Message != "" {
			t.cycleNodeRequest.Status.Message += ", "
		}

		t.cycleNodeRequest.Status.Message += err.Error()
	}

	// handle conflicts before complaining
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return t.rm.UpdateObject(t.cycleNodeRequest)
	}); err != nil {
		t.rm.Logger.Error(err, "unable to update cycleNodeRequest")
	}

	// Notify that the cycling has transitioned phase
	if t.rm.Notifier != nil {
		if err := t.rm.Notifier.PhaseTransitioned(t.cycleNodeRequest); err != nil {
			t.rm.Logger.Error(err, "Unable to post message to messaging provider", "phase", t.cycleNodeRequest.Status.Phase)
		}
	}

	return reconcile.Result{}, err
}
