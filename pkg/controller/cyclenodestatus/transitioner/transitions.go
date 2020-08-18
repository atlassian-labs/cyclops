package transitioner

import (
	"fmt"
	"time"

	"strings"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	// How long we will try to cycle this one node for before giving up
	nodeTerminationGracePeriod = 180 * time.Minute
)

// transitionUndefined transitions any CycleNodeStatuses in the Undefined phase to the Pending phase
// It checks to ensure that a node name has been provided
// When the CRD validation features are available in the Kubernetes API, we could probably remove these checks
func (t *CycleNodeStatusTransitioner) transitionUndefined() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeStatus, "Initialising", "Initialising CycleNodeStatus")

	// Check to ensure method is set
	if len(t.cycleNodeStatus.Spec.CycleSettings.Method) == 0 {
		return t.transitionToFailed(fmt.Errorf("method cannot be empty"))
	}
	// This CRD should only be created by the controller, so we don't validate the method any further

	// Check to ensure NodeName is set
	if len(t.cycleNodeStatus.Spec.NodeName) == 0 {
		return t.transitionToFailed(fmt.Errorf("nodeName cannot be empty"))
	}

	// Set the timestamp so we know when we've timed out
	currentTime := metav1.Now()
	t.cycleNodeStatus.Status.StartedTimestamp = &currentTime

	// Transition the object to pending
	return t.transitionObject(v1.CycleNodeStatusPending)
}

// transitionPending transitions any CycleNodeStatuses in the Pending phase into either the WaitingPods phase, or the
// RemovingLabelsFromPods phase based on the .Spec.Method provided.
// Gets the requested node from the cloud provider and from kube and performs sanity checks. Depending on these checks
// the CycleNodeStatus may go straight to Failed or Successful.
//    If the node has problems then it will transition straight to Failed.
func (t *CycleNodeStatusTransitioner) transitionPending() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeStatus, "FetchingNode", "Fetching information about node: %v", t.cycleNodeStatus.Spec.NodeName)
	node, err := t.rm.GetNode(t.cycleNodeStatus.Spec.NodeName)
	if err != nil {
		// If the node doesn't exist in Kube then assume that the node was killed by something else
		// Don't allow this to fail the CycleNodeRequest
		if serr, ok := err.(*errors.StatusError); ok && errors.IsNotFound(serr) {
			t.rm.LogEvent(t.cycleNodeStatus, "FetchingNode", "Node not found, assuming cycle successful: %v", t.cycleNodeStatus.Spec.NodeName)
			return t.transitionToSuccessful()
		}
		return t.transitionToFailed(err)
	}

	// Set the current node
	t.cycleNodeStatus.Status.CurrentNode.Name = node.Name
	t.cycleNodeStatus.Status.CurrentNode.ProviderID = node.Spec.ProviderID

	// Ensure the node still exists in AWS before attempting anything
	existingProviderIDs, err := t.rm.CloudProvider.InstancesExist([]string{t.cycleNodeStatus.Status.CurrentNode.ProviderID})
	if err != nil {
		// The node existed in Kube if we got this far, so if it doesn't exist in AWS then something funky is
		// happening, so we exit with an error
		return t.transitionToFailed(err)
	}
	if len(existingProviderIDs) == 0 {
		return t.transitionToSuccessful()
	}

	// Depending on the Method we transition to a different phase
	if t.cycleNodeStatus.Spec.CycleSettings.Method == v1.CycleNodeRequestMethodWait {
		return t.transitionObject(v1.CycleNodeStatusWaitingPods)
	}
	return t.transitionObject(v1.CycleNodeStatusRemovingLabelsFromPods)
}

// transitionWaitingPods transitions any CycleNodeStatuses in the WaitingPods phase to the
// RemovingLabelsFromPods phase. Waits for any pods not excluded by the WaitRules for this CycleNodeStatus
// to finish then transitions to the next phase.
func (t *CycleNodeStatusTransitioner) transitionWaitingPods() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeStatus, "WaitingPods", "Waiting for pods to finish")
	finished, err := t.podsFinished()
	if err != nil {
		return t.transitionToFailed(err)
	}
	if !finished {
		if t.timedOut() {
			return t.transitionToFailed(fmt.Errorf("timed out waiting for pods to finish"))
		}
		return reconcile.Result{Requeue: true, RequeueAfter: 60 * time.Second}, nil
	}

	return t.transitionObject(v1.CycleNodeStatusRemovingLabelsFromPods)
}

// transitionRemovingLabelsFromPods transitions a CycleNodeStatus in the RemovingLabelsFromPods phase to the Draining phase.
// This removes any of the specified labels from pods that have them.
// This is used to remove the pod from any services/endpoints before pod termination, so that
// it is guaranteed there is no traffic sent to it when draining.
func (t *CycleNodeStatusTransitioner) transitionRemovingLabelsFromPods() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeStatus, "RemovingLabels", "Removing labels from pods")
	finished, err := t.removeLabelsFromPods()
	if err != nil {
		return t.transitionToFailed(err)
	}
	if !finished {
		return reconcile.Result{Requeue: true, RequeueAfter: 1 * time.Second}, nil
	}

	return t.transitionObject(v1.CycleNodeStatusDrainingPods)
}

// transitionDraining transitions any CycleNodeStatuses in the Draining phase to the Deleting phase
// It gets all of the drainable pods on the selected node and then drains them.
// It will check that all of the pods on the selected node have been drained before moving it to the
// deleting phase..
func (t *CycleNodeStatusTransitioner) transitionDraining() (reconcile.Result, error) {
	// Drain pods off the node
	t.rm.LogEvent(t.cycleNodeStatus, "DrainingPods", "Draining pods from node: %v", t.cycleNodeStatus.Status.CurrentNode.Name)
	finished, errs := t.rm.DrainPods(t.cycleNodeStatus.Status.CurrentNode.Name)

	// We need to do some fairly complicated error handling here. It is most efficient to drain all pods at once, as
	// this stops us being blocked behind one pod that takes a long time to get evicted. This means we need to handle
	// all the errors at once. One class of error, StatusTooManyRequests, indicates that the evicted pod is
	// "undisruptable" via a pod disruption budget. This error is fine. All others are not, and we have to combine
	// them and fail this CycleNodeStatus if we encounter them.
	var unexpectedErrors []string
	tooManyRequests := false
	for _, err := range errs {
		if err != nil {
			// Custom logic handling, mainly for handling pods that are undisruptable via a PodDisruptionBudget
			if serr, ok := err.(*errors.StatusError); ok && errors.IsTooManyRequests(serr) {
				// API says we should retry, we do the actual retry further down but log the pod here
				t.rm.Logger.Info("waiting to retry after receiving StatusTooManyRequests error",
					"podName", serr.ErrStatus.Details.Name)
				tooManyRequests = true
			} else {
				unexpectedErrors = append(unexpectedErrors, err.Error())
			}
		}
	}
	// Fail with all of the combined encountered errors if we got any. If we failed inside the loop we would
	// potentially miss some important information in the logs.
	if len(unexpectedErrors) > 0 {
		return t.transitionToFailed(fmt.Errorf(strings.Join(unexpectedErrors, "\n")))
	}
	// No serious errors were encountered. If we're done, move on.
	if finished {
		return t.transitionObject(v1.CycleNodeStatusDeletingNode)
	}

	// Fail if we've taken too long in this phase.
	if t.timedOut() {
		return t.transitionToFailed(fmt.Errorf("timed out while draining pods"))
	}
	// The API says we should retry (likely due to currently undisruptable pods)
	if tooManyRequests {
		return reconcile.Result{Requeue: true, RequeueAfter: 15 * time.Second}, nil
	}
	// If all the pods aren't finished draining, try again a while later to avoid spamming the API server.
	return reconcile.Result{Requeue: true, RequeueAfter: 30 * time.Second}, nil
}

// transitionDeleting transitions any CycleNodeStatuses in the Deleting phase to the Terminating phase
// It will delete the node out of the Kubernetes API.
func (t *CycleNodeStatusTransitioner) transitionDeleting() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeStatus, "DeletingNode", "Deleting node: %v", t.cycleNodeStatus.Status.CurrentNode.Name)
	err := t.rm.DeleteNode(t.cycleNodeStatus.Status.CurrentNode.Name)
	if err != nil {
		return t.transitionToFailed(err)
	}

	return t.transitionObject(v1.CycleNodeStatusTerminatingNode)
}

// transitionTerminating transitions any CycleNodeStatuses in the Terminating phase to the Successful phase.
// It terminates the node via the cloud provider.
func (t *CycleNodeStatusTransitioner) transitionTerminating() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeStatus, "TerminatingNode", "Terminating instance: %v", t.cycleNodeStatus.Status.CurrentNode.ProviderID)
	err := t.rm.CloudProvider.TerminateInstance(t.cycleNodeStatus.Status.CurrentNode.ProviderID)
	if err != nil {
		return t.transitionToFailed(err)
	}

	return t.transitionObject(v1.CycleNodeStatusSuccessful)
}

// transitionFailed handles failed CycleNodeStatuses
func (t *CycleNodeStatusTransitioner) transitionFailed() (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

// transitionSuccessful handles successful CycleNodeStatuses
func (t *CycleNodeStatusTransitioner) transitionSuccessful() (reconcile.Result, error) {
	return reconcile.Result{}, nil
}
