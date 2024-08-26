package transitioner

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/pkg/errors"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	scaleUpWait              = 1 * time.Minute
	scaleUpLimit             = 20 * time.Minute
	nodeEquilibriumWaitLimit = 5 * time.Minute
)

// transitionUndefined transitions any CycleNodeRequests in the undefined phase to the pending phase
// It checks to ensure that a valid selector has been provided.
func (t *CycleNodeRequestTransitioner) transitionUndefined() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeRequest, "Initialising", "Initialising cycleNodeRequest")

	if t.rm.Notifier != nil {
		if err := t.rm.Notifier.CyclingStarted(t.cycleNodeRequest); err != nil {
			t.rm.Logger.Error(err, "Unable to post message to messaging provider", "phase", t.cycleNodeRequest.Status.Phase)
		}
	}

	// Check fields on the cycleNodeRequest for validity. We rely on the CRD validation rules
	// to do most of the work here for us.

	// Check to ensure a label selector has been provided
	if t.cycleNodeRequest.Spec.Selector.Size() == 0 {
		return t.transitionToHealing(fmt.Errorf("selector cannot be empty"))
	}

	// Protect against failure case where cyclops checks for leftover CycleNodeStatus objects using the CycleNodeRequest name in the label selector
	// Label values must be no more than 63 characters long
	validationErrors := validation.IsDNS1035Label(t.cycleNodeRequest.Name)

	if len(validationErrors) > 0 {
		return t.transitionToFailed(fmt.Errorf(strings.Join(validationErrors, ",")))
	}

	// Transition the object to pending
	return t.transitionObject(v1.CycleNodeRequestPending)
}

// transitionPending transitions any CycleNodeRequests in the pending phase to the initialised phase
// Does the following:
//  1. fetches the current nodes by the label selector, and saves them as nodes to be terminated
//  2. describes the node group and checks that the number of instances in the node group matches the number we
//     are planning on terminating
func (t *CycleNodeRequestTransitioner) transitionPending() (reconcile.Result, error) {
	// Start the equilibrium wait timer, if this times out then the set of nodes in kube and
	// the cloud provider is not considered valid. Transition to the Healing phase as cycling
	// should not proceed.
	if err := t.errorIfEquilibriumTimeoutReached(); err != nil {
		return t.transitionToHealing(err)
	}

	// Fetch the node names for the cycleNodeRequest, using the label selector provided
	t.rm.LogEvent(t.cycleNodeRequest, "SelectingNodes", "Selecting nodes with label selector")

	// Find all the nodes in kube and the cloud provider nodegroups selected by the CNR. These
	// should be all the nodes in each, regardless of it they exist in both.
	kubeNodes, nodeGroupInstances, err := t.findAllNodesForCycle()
	if err != nil {
		return t.transitionToHealing(err)
	}

	// Find all the nodes nodes that exist in both kube and the cloud provider nodegroups. This is
	// the valid set of nodes and can be worked on. This is an AND condition on the two initial
	// sets of nodes.
	validKubeNodes, validNodeGroupInstances := findValidNodes(kubeNodes, nodeGroupInstances)

	// Find all the nodes that exist in either kube or the cloud provider nodegroups, but not both.
	// The nodes in the cloud provider can either not exist or be detached from one of the nodegroups
	// and this will be determined when dealt with. This is an XOR condition on the two initial sets
	// of nodes.
	nodesNotInCloudProviderNodegroup, instancesNotInKube := findProblemNodes(kubeNodes, nodeGroupInstances)

	// If the node state isn't correct then go through and attempt to fix it. The steps in this block
	// attempt to fix the node state and then requeues the Pending phase to re-check. It is very
	// possible that the node state changes during the steps and it cannot be fixed. Hopefully after
	// a few runs the state can be fixed.
	if len(nodesNotInCloudProviderNodegroup) > 0 || len(instancesNotInKube) > 0 {
		t.logProblemNodes(nodesNotInCloudProviderNodegroup, instancesNotInKube)

		// Try to fix the case where there are 1 or more instances matching the node selector for the
		// nodegroup in kube but are not attached to the nodegroup in the cloud provider by
		// re-attaching them.
		if err := t.reattachAnyDetachedInstances(nodesNotInCloudProviderNodegroup); err != nil {
			return t.transitionToHealing(err)
		}

		// Try to fix the case where there are 1 or more kube node objects without any matching
		// running instances in the cloud provider. This could be because of the finalizer that
		// was added during a previous failed cycle.
		if err := t.deleteAnyOrphanedKubeNodes(nodesNotInCloudProviderNodegroup); err != nil {
			return t.transitionToHealing(err)
		}

		// After working through these attempts, requeue to run through the Pending phase from the
		// beginning to check the full state of nodes again. If there are any problem nodes we should
		// not proceed and keep requeuing until the state is fixed or the timeout has been reached.
		return reconcile.Result{Requeue: true, RequeueAfter: requeueDuration}, nil
	}

	valid, err := t.validateInstanceState(validNodeGroupInstances)
	if err != nil {
		return t.transitionToHealing(err)
	}

	if !valid {
		t.rm.Logger.Info("instance state not valid, requeuing")
		return reconcile.Result{Requeue: true, RequeueAfter: requeueDuration}, nil
	}

	t.rm.Logger.Info("instance state valid, proceeding")

	// make a list of the nodes to terminate
	if len(t.cycleNodeRequest.Spec.NodeNames) > 0 {
		// If specific node names are provided, check they actually exist in the node group
		t.rm.LogEvent(t.cycleNodeRequest, "SelectingNodes", "Adding named nodes to NodesToTerminate")
		err := t.addNamedNodesToTerminate(validKubeNodes, validNodeGroupInstances)
		if err != nil {
			return t.transitionToHealing(err)
		}
	} else {
		// Otherwise just add all the nodes in the node group
		t.rm.LogEvent(t.cycleNodeRequest, "SelectingNodes", "Adding all node group nodes to NodesToTerminate")

		for _, kubeNode := range validKubeNodes {
			// Check to ensure the kubeNode object maps to an existing node in the ASG
			// If this isn't the case, this is a phantom node. Fail the cnr to be safe.
			nodeGroupName, ok := validNodeGroupInstances[kubeNode.Spec.ProviderID]
			if !ok {
				return t.transitionToHealing(fmt.Errorf("kubeNode %s not found in the list of instances in the ASG", kubeNode.Name))
			}

			t.cycleNodeRequest.Status.NodesAvailable = append(
				t.cycleNodeRequest.Status.NodesAvailable,
				newCycleNodeRequestNode(&kubeNode, nodeGroupName.NodeGroupName()),
			)

			t.cycleNodeRequest.Status.NodesToTerminate = append(
				t.cycleNodeRequest.Status.NodesToTerminate,
				newCycleNodeRequestNode(&kubeNode, nodeGroupName.NodeGroupName()),
			)
		}
	}

	if len(t.cycleNodeRequest.Spec.HealthChecks) > 0 {
		if err = t.performInitialHealthChecks(validKubeNodes); err != nil {
			return t.transitionToHealing(err)
		}
	}

	// If the concurrency isn't provided, then default it to the number of nodesToTerminate
	if t.cycleNodeRequest.Spec.CycleSettings.Concurrency <= 0 {
		t.cycleNodeRequest.Spec.CycleSettings.Concurrency = int64(len(t.cycleNodeRequest.Status.NodesToTerminate))
	}

	// Remove any children that may be left over from previous runs. Should most often be a no-op.
	// We do this after all the other error checking to avoid changing cluster state unless we would actually
	// be acting on this request.
	if err := t.removeOldChildrenFromCluster(); err != nil {
		return t.transitionToHealing(err)
	}

	return t.transitionObject(v1.CycleNodeRequestInitialised)
}

// transitionInitialised transitions any CycleNodeRequests in the initialised phase to the ScalingUp phase
// If there aren't any more nodes that need to be cycled, it transitions straight to successful.
// It detaches a number of nodes from the node group, based on the available concurrency, which will
// trigger the cloud provider to create a new node in the old node's AZs.
func (t *CycleNodeRequestTransitioner) transitionInitialised() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeRequest, "SelectingNodes", "Selecting nodes to terminate")

	// The maximum nodes we can select are bounded by our concurrency. We take into account the number
	// of nodes we are already working on, and only introduce up to our concurrency cap more nodes in this step.
	maxNodesToSelect := t.cycleNodeRequest.Spec.CycleSettings.Concurrency - t.cycleNodeRequest.Status.ActiveChildren

	t.rm.Logger.Info("Selecting nodes to terminate", "numNodes", maxNodesToSelect)

	nodes, numNodesInProgress, err := t.getNodesToTerminate(maxNodesToSelect)
	if err != nil {
		return t.transitionToHealing(err)
	}

	t.cycleNodeRequest.Status.NumNodesCycled = len(t.cycleNodeRequest.Status.NodesToTerminate) - len(t.cycleNodeRequest.Status.NodesAvailable) - numNodesInProgress - len(nodes)

	// Check if we can transition to WaitingTermination or Successful
	if transitioning, reconcileResult, err := t.checkIfTransitioning(len(nodes), numNodesInProgress); transitioning {
		t.rm.Logger.Info("No more valid nodes in kube left to cycle")
		return reconcileResult, err
	}

	nodeGroups, err := t.rm.CloudProvider.GetNodeGroups(t.cycleNodeRequest.GetNodeGroupNames())
	if err != nil {
		return t.transitionToHealing(err)
	}

	readyInstances := nodeGroups.ReadyInstances()
	validProviderIDs := map[string]bool{}

	for _, node := range nodes {
		for _, readyInstance := range readyInstances {
			if readyInstance.MatchesProviderID(node.Spec.ProviderID) {
				validProviderIDs[node.Spec.ProviderID] = true
			}
		}
	}

	t.cycleNodeRequest.Status.NumNodesCycled = len(t.cycleNodeRequest.Status.NodesToTerminate) - len(t.cycleNodeRequest.Status.NodesAvailable) - numNodesInProgress - len(validProviderIDs)

	// This is done a second time to account for a race condition where an instance on cloud provider is no longer running but is still registered in kube
	// If the check were performed before the transition to WaitingTermination above, cyclops would perform many requests and eventually get rate limited by cloud provider
	if transitioning, reconcileResult, err := t.checkIfTransitioning(len(validProviderIDs), numNodesInProgress); transitioning {
		t.rm.Logger.Info("No more valid nodes in the cloud provider left to cycle")
		return reconcileResult, err
	}

	// Set the current nodes we're working on. The list is already limited to our
	// desired concurrency.
	t.cycleNodeRequest.Status.CurrentNodes = []v1.CycleNodeRequestNode{}

	for _, node := range nodes {
		if _, ok := validProviderIDs[node.Spec.ProviderID]; ok {
			t.cycleNodeRequest.Status.CurrentNodes = append(
				t.cycleNodeRequest.Status.CurrentNodes,
				newCycleNodeRequestNode(node, readyInstances[node.Spec.ProviderID].NodeGroupName()),
			)
		}
	}

	// Post a notification showing the new nodes selected for cycling
	if t.rm.Notifier != nil {
		if err := t.rm.Notifier.NodesSelected(t.cycleNodeRequest); err != nil {
			t.rm.Logger.Error(err, "Unable to post message to messaging provider", "phase", t.cycleNodeRequest.Status.Phase)
		}

		if err := t.rm.UpdateObject(t.cycleNodeRequest); err != nil {
			return t.transitionToHealing(err)
		}
	}

	// Detach the nodes from the nodes group - this will trigger a replacement, and start the scale up
	// Detach each node independently so that valid nodes are not affected by invalid nodes
	t.rm.LogEvent(t.cycleNodeRequest, "DetachingNodes", "Detaching instances from nodes group: %v", t.cycleNodeRequest.Status.CurrentNodes)
	var validNodes []v1.CycleNodeRequestNode

	for _, node := range t.cycleNodeRequest.Status.CurrentNodes {
		t.rm.Logger.Info("Adding finalizer to node", "node", node.Name)

		// Add the finalizer to the node before detaching it
		if err := t.rm.AddFinalizerToNode(node.Name); err != nil {
			t.rm.LogEvent(t.cycleNodeRequest, "AddFinalizerToNodeError", err.Error())
			return t.transitionToHealing(err)
		}

		t.rm.Logger.Info("Adding annotation to node", "node", node.Name)

		// Add the nodegroup annotation to the node before detaching it
		if err := t.rm.AddNodegroupAnnotationToNode(node.Name, node.NodeGroupName); err != nil {
			t.rm.LogEvent(t.cycleNodeRequest, "AddAnnotationToNodeError", err.Error())
			return t.transitionToHealing(err)
		}

		alreadyDetaching, err := nodeGroups.DetachInstance(node.ProviderID)

		if alreadyDetaching {
			t.rm.LogEvent(t.cycleNodeRequest, "RaceCondition", "Node %v was already detaching from the ASG.", node.Name)
			continue
		}

		// Catch any error which is not the result of the node already detaching
		if err != nil {
			t.rm.LogEvent(t.cycleNodeRequest, "DetachingNodeError", err.Error())
			return t.transitionToHealing(err)
		}

		// Only keep track of valid nodes before moving to the ScalingUp state
		validNodes = append(validNodes, node)
	}

	t.cycleNodeRequest.Status.CurrentNodes = validNodes

	// Set the scale up started time
	currentTime := metav1.Now()
	t.cycleNodeRequest.Status.ScaleUpStarted = &currentTime
	return t.transitionObject(v1.CycleNodeRequestScalingUp)
}

// transitionScalingUp transitions any CycleNodeRequests in the ScalingUp phase to the Cordoning phase.
// It waits until the nodes that were requested have joined Kubernetes and are "Ready".
func (t *CycleNodeRequestTransitioner) transitionScalingUp() (reconcile.Result, error) {
	scaleUpStarted := t.cycleNodeRequest.Status.ScaleUpStarted

	// Check we have waited long enough - give the node some time to start up
	if time.Since(scaleUpStarted.Time) <= scaleUpWait {
		t.rm.LogEvent(t.cycleNodeRequest, "ScalingUpWaiting", "Waiting for new nodes to be warmed up")
		return reconcile.Result{Requeue: true, RequeueAfter: scaleUpWait}, nil
	}

	nodeGroups, err := t.rm.CloudProvider.GetNodeGroups(t.cycleNodeRequest.GetNodeGroupNames())
	if err != nil {
		return t.transitionToHealing(err)
	}
	// If we have exceeded the max scale up time, then fail
	if scaleUpStarted.Add(scaleUpLimit).Before(time.Now()) {
		return t.transitionToHealing(
			fmt.Errorf("all nodes failed to come up in time - instances not ready in cloud provider: %+v",
				nodeGroups.NotReadyInstances()))
	}

	// Ensure all kubernetes nodes are ready
	kubeNodes, err := t.listReadyNodes(false)
	if err != nil {
		return t.transitionToHealing(err)
	}

	// Check if all our instances are ready.
	// Because the scale up uses instance detachment, which does not change the current size of the node group,
	// we require the number of nodes in the node group plus the size of the last request of nodes (which are no
	// longer present in the node group). If all of these nodes are "Ready" in Kubernetes then the scale up has
	// succeeded.

	numKubeNodesReady := len(kubeNodes)
	var nodesToRemove []v1.CycleNodeRequestNode

	// Increase the kubeNode count requirement by the number of nodes which are observed to have been removed prematurely
	for _, node := range t.cycleNodeRequest.Status.CurrentNodes {
		if _, instanceFound := kubeNodes[node.ProviderID]; !instanceFound {
			nodesToRemove = append(nodesToRemove, node)
			numKubeNodesReady++
		}
	}

	requiredNumNodes := len(nodeGroups.Instances()) + len(t.cycleNodeRequest.Status.CurrentNodes)
	allInstancesReady := len(nodeGroups.ReadyInstances()) >= len(nodeGroups.Instances())
	allKubernetesNodesReady := numKubeNodesReady >= requiredNumNodes

	// If something scales down/up right at this moment then the overall maths still works, because the
	// instances we're working on are detached from the node group.
	t.rm.Logger.Info("Waiting for new nodes to be ready",
		"numReadyInstances", len(nodeGroups.ReadyInstances()),
		"numInstances", len(nodeGroups.Instances()),
		"requiredNumNodes", requiredNumNodes,
		"numKubeNodesReady", numKubeNodesReady,
		"scalingUpBy", len(t.cycleNodeRequest.Status.CurrentNodes))

	// If either check isn't ready, requeue to check again later
	if !(allInstancesReady && allKubernetesNodesReady) {
		t.rm.LogEvent(t.cycleNodeRequest, "ScalingUpWaiting", "Waiting for new nodes to be ready")
		return reconcile.Result{Requeue: true, RequeueAfter: requeueDuration}, nil
	}

	// Remove any nodes from the CNR object which are found to have been removed prematurely due to a race condition
	for _, nodeToRemove := range nodesToRemove {
		for i, node := range t.cycleNodeRequest.Status.CurrentNodes {
			if nodeToRemove.Name == node.Name {
				t.rm.LogEvent(t.cycleNodeRequest, "RaceCondition", "Node %v was prematurely terminated.", node.Name)
				t.cycleNodeRequest.Status.CurrentNodes = append(t.cycleNodeRequest.Status.CurrentNodes[:i],
					t.cycleNodeRequest.Status.CurrentNodes[i+1:]...)
				break
			}
		}
	}

	// Skip looping through nodes if no health checks need to be performed
	if len(t.cycleNodeRequest.Spec.HealthChecks) > 0 {
		allHealthChecksPassed, err := t.performCyclingHealthChecks(kubeNodes)
		if err != nil {
			return t.transitionToHealing(err)
		}

		if !allHealthChecksPassed {
			// Reconcile any health checks passed to the cnr object
			if err := t.rm.UpdateObject(t.cycleNodeRequest); err != nil {
				return t.transitionToHealing(err)
			}

			return reconcile.Result{Requeue: true, RequeueAfter: requeueDuration}, nil
		}
	}

	t.rm.LogEvent(t.cycleNodeRequest, "ScalingUpCompleted", "New nodes are now ready")
	return t.transitionObject(v1.CycleNodeRequestCordoningNode)
}

// transitionCordoning transitions any CycleNodeRequests in the Cordoning phase to the WaitingTermination phase.
// It cordons the nodes selected for termination and creates a CycleNodeStatus CRD for each of them
// to track the node-specific draining work.
func (t *CycleNodeRequestTransitioner) transitionCordoning() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeRequest, "CordoningNodes", "Cordoning nodes: %v", t.cycleNodeRequest.Status.CurrentNodes)

	if t.cycleNodeRequest.Spec.SkipPreTerminationChecks {
		t.rm.Logger.Info("Skipping pre-termination checks")
	}

	allNodesReadyForTermination := true
	for _, node := range t.cycleNodeRequest.Status.CurrentNodes {
		// If the node is not already cordoned, cordon it
		cordoned, err := k8s.IsCordoned(node.Name, t.rm.RawClient)
		// Skip handling the node if it doesn't exist
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			t.rm.Logger.Error(err, "failed to check if node is cordoned", "nodeName", node.Name)
			return t.transitionToHealing(err)
		}
		if !cordoned {
			if err := k8s.CordonNode(node.Name, t.rm.RawClient); err != nil {
				return t.transitionToHealing(err)
			}
		}

		// Perform pre-termination checks after the node is cordoned
		// Cruicially, do this before the CNS is created for node to begin termination
		if !t.cycleNodeRequest.Spec.SkipPreTerminationChecks && len(t.cycleNodeRequest.Spec.PreTerminationChecks) > 0 {
			// Try to send the trigger, if is has already been sent then this will
			// be skipped in the function. The trigger must only be sent once
			if err := t.sendPreTerminationTrigger(node); err != nil {
				t.rm.LogEvent(t.cycleNodeRequest,
					"PreTerminationTriggerFailed", "failed to send pre-termination trigger to %s, err: %v", node.Name, err)
				return t.transitionToHealing(errors.Wrapf(err, "failed to send pre-termination trigger to %s", node.Name))
			}

			// After the trigger has been sent, perform health checks to monitor if the node
			// can be terminated. If all checks pass then it can be terminated.
			allHealthChecksPassed, err := t.performPreTerminationHealthChecks(node)
			if err != nil {
				t.rm.LogEvent(t.cycleNodeRequest, "PreTerminationHealChecks",
					"failed to perform pre-termination health checks for %v, err: %v", node.Name, err)
				return t.transitionToHealing(errors.Wrapf(err, "failed to perform pre-termination health checks for %s", node.Name))
			}

			// If not all health checks have passed, it is not ready for termination yet
			// But we can continue to trigger checks on the other nodes
			if !allHealthChecksPassed {
				allNodesReadyForTermination = false
				continue
			}
		}

		// Create a CycleNodeStatus CRD to start the termination process
		if err := t.rm.Client.Create(context.TODO(), t.makeCycleNodeStatusForNode(node.Name)); err != nil {
			return t.transitionToHealing(err)
		}

		// Add a label to the node to show that we've started working on it
		if err := k8s.AddLabelToNode(node.Name, cycleNodeLabel, t.cycleNodeRequest.Name, t.rm.RawClient); err != nil {
			t.rm.Logger.Error(err, "patch failed: could not add cyclops label to node", "nodeName", node.Name)
			return t.transitionToHealing(err)
		}
	}

	// If not all nodes are ready for termination, requeue the CNR to try again
	if !allNodesReadyForTermination {
		// Reconcile any health checks passed to the cnr object
		if err := t.rm.UpdateObject(t.cycleNodeRequest); err != nil {
			return t.transitionToHealing(err)
		}

		return reconcile.Result{Requeue: true, RequeueAfter: requeueDuration}, nil
	}

	// The scale up + cordon is finished, we no longer need this list of nodes
	t.cycleNodeRequest.Status.CurrentNodes = []v1.CycleNodeRequestNode{}
	return t.transitionObject(v1.CycleNodeRequestWaitingTermination)
}

// transitionWaitingTermination transitions any CycleNodeRequests in the WaitingTermination phase
// to the Initialising phase, ready to queue more instances.
// The CycleNodeRequest will remain in the WaitingTermination phase until there are enough nodes finished terminating
// to trigger another ScaleUp operation.
func (t *CycleNodeRequestTransitioner) transitionWaitingTermination() (reconcile.Result, error) {
	t.rm.LogEvent(t.cycleNodeRequest, "WaitingTermination", "Waiting for instances to terminate")

	// While there are CycleNodeStatus objects not in Failed or Successful, stay in this phase and wait for them
	// to finish.
	desiredPhase, err := t.reapChildren()
	// If any are in Failed phase then this CycleNodeRequest will be sent to the Failed phase, where it will
	// continue to reap it's children.
	if err != nil {
		return t.transitionToHealing(err)
	}

	if err := t.rm.UpdateObject(t.cycleNodeRequest); err != nil {
		return t.transitionToHealing(err)
	}

	return t.transitionObject(desiredPhase)
}

// transitionHealing handles healing CycleNodeRequests
func (t *CycleNodeRequestTransitioner) transitionHealing() (reconcile.Result, error) {
	nodeGroups, err := t.rm.CloudProvider.GetNodeGroups(t.cycleNodeRequest.GetNodeGroupNames())
	if err != nil {
		return t.transitionToFailed(err)
	}

	for _, node := range t.cycleNodeRequest.Status.NodesToTerminate {
		// nodes in NodesToTerminate may have been terminated, so check if they still exist
		nodeExists, err := k8s.NodeExists(node.Name, t.rm.RawClient)
		if err != nil {
			return t.transitionToFailed(err)
		}

		if !nodeExists {
			t.rm.LogEvent(t.cycleNodeRequest,
				"HealingNodes", "Node does not exist, skip healing node: %s", node.Name)
			continue
		}

		if err := t.rm.RemoveFinalizerFromNode(node.Name); err != nil {
			t.rm.LogEvent(t.cycleNodeRequest, "RemoveFinalizerFromNodeError", err.Error())
			return t.transitionToFailed(err)
		}

		// try and re-attach the nodes, if any were un-attached
		t.rm.LogEvent(t.cycleNodeRequest, "AttachingNodes", "Attaching instances to nodes group: %v", node.Name)
		// if the node is already attached, ignore the error and continue to un-cordoning, otherwise return with error
		alreadyAttached, err := nodeGroups.AttachInstance(node.ProviderID, node.NodeGroupName)
		if err != nil && !alreadyAttached {
			return t.transitionToFailed(err)
		}
		if alreadyAttached {
			t.rm.LogEvent(t.cycleNodeRequest,
				"AttachingNodes", "Skip re-attaching instances to nodes group: %v, err: %v",
				node.Name, err)
		}

		// un-cordon after attach as well
		t.rm.LogEvent(t.cycleNodeRequest, "UncordoningNodes", "Uncordoning nodes in node group: %v", node.Name)

		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return k8s.UncordonNode(node.Name, t.rm.RawClient)
		})

		if apierrors.IsNotFound(err) {
			continue
		}

		if err != nil {
			return t.transitionToFailed(err)
		}
	}

	return t.transitionToFailed(nil)
}

// transitionFailed handles failed CycleNodeRequests
func (t *CycleNodeRequestTransitioner) transitionFailed() (reconcile.Result, error) {
	shouldRequeue, err := t.finalReapChildren()
	if err != nil {
		return t.transitionToFailed(err)
	}
	if shouldRequeue {
		return reconcile.Result{Requeue: true, RequeueAfter: transitionDuration}, nil
	}

	return reconcile.Result{}, nil
}

// transitionSuccessful handles successful CycleNodeRequests
func (t *CycleNodeRequestTransitioner) transitionSuccessful() (reconcile.Result, error) {
	shouldRequeue, err := t.finalReapChildren()
	if err != nil {
		return t.transitionToHealing(err)
	}

	if shouldRequeue {
		return reconcile.Result{Requeue: true, RequeueAfter: transitionDuration}, nil
	}

	// Delete failed sibling CNRs regardless of whether the CNR for the
	// transitioner should be deleted. If failed CNRs pile up that will prevent
	// Cyclops observer from auto-generating new CNRs for a nodegroup.
	if err := t.deleteFailedSiblingCNRs(); err != nil {
		return t.transitionToHealing(err)
	}

	// If deleting CycleNodeRequests is not enabled, stop here
	if !t.options.DeleteCNR {
		return reconcile.Result{}, nil
	}

	// Delete CycleNodeRequests that have reaped all of their children and are older
	// than the time configured to keep them for.
	if t.cycleNodeRequest.CreationTimestamp.Add(t.options.DeleteCNRExpiry).Before(time.Now()) {
		t.rm.Logger.Info("Deleting CycleNodeRequest")

		if err := t.rm.Client.Delete(context.TODO(), t.cycleNodeRequest); err != nil {
			t.rm.Logger.Error(err, "Unable to delete expired CycleNodeRequest")
		}

		return reconcile.Result{}, nil
	}

	// Requeue them for checking later if the expiry has been reached
	t.rm.Logger.Info("Requeuing CycleNodeRequest for deleting later")
	return reconcile.Result{Requeue: true, RequeueAfter: t.options.DeleteCNRRequeue}, nil
}
