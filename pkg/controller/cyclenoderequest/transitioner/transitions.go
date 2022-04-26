package transitioner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	scaleUpWait              = 1 * time.Minute
	scaleUpLimit             = 20 * time.Minute
	nodeEquilibriumWaitLimit = 5 * time.Minute
)

const ForceAsgConfigCheckAnnotation = "force-check-asg-config"


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
// 1. fetches the current nodes by the label selector, and saves them as nodes to be terminated
// 2. describes the node group and checks that the number of instances in the node group matches the number we
//    are planning on terminating
func (t *CycleNodeRequestTransitioner) transitionPending() (reconcile.Result, error) {
	// Fetch the node names for the cycleNodeRequest, using the label selector provided
	t.rm.LogEvent(t.cycleNodeRequest, "SelectingNodes", "Selecting nodes with label selector")
	kubeNodes, err := t.listReadyNodes(true)
	if err != nil {
		return t.transitionToHealing(err)
	}
	if len(kubeNodes) == 0 {
		return t.transitionToHealing(fmt.Errorf("no nodes matched selector"))
	}

	// Only retain nodes which still exist inside cloud provider
	var nodeProviderIDs []string

	for _, node := range kubeNodes {
		nodeProviderIDs = append(nodeProviderIDs, node.Spec.ProviderID)
	}

	existingProviderIDs, err := t.rm.CloudProvider.InstancesExist(nodeProviderIDs)
	if err != nil {
		return t.transitionToHealing(errors.Wrap(err, "failed to check instances that exist from cloud provider"))
	}
	var existingKubeNodes []corev1.Node

	for _, node := range kubeNodes {
		for _, validProviderID := range existingProviderIDs {
			if node.Spec.ProviderID == validProviderID {
				existingKubeNodes = append(existingKubeNodes, node)
				break
			}
		}
	}

	kubeNodes = existingKubeNodes

	if len(kubeNodes) == 0 {
		return t.transitionToHealing(fmt.Errorf("no existing nodes in cloud provider matched selector"))
	}

	// Describe the node group for the request
	t.rm.LogEvent(t.cycleNodeRequest, "FetchingNodeGroup", "Fetching node group: %v", t.cycleNodeRequest.GetNodeGroupNames())
	nodeGroups, err := t.rm.CloudProvider.GetNodeGroups(t.cycleNodeRequest.GetNodeGroupNames())
	if err != nil {
		return t.transitionToHealing(err)
	}

	// get instances inside cloud provider node groups
	nodeGroupInstances := nodeGroups.Instances()

	forceCheckAsgConfigAnnotations, _ := strconv.ParseBool(t.cycleNodeRequest.Annotations[ForceAsgConfigCheckAnnotation])
	if forceCheckAsgConfigAnnotations {
		var skipped = true
		for _, instance := range nodeGroupInstances {
			outOfDate := instance.OutOfDate()
			if outOfDate {
				skipped = false
				break
			}
		}
		if skipped {
			t.cycleNodeRequest.Status.Message = "Cycling skipped because all nodes in the nodegroups are up-to-date with a cloud provider ASG"
			return t.transitionObject(v1.CycleNodeRequestSkipped)
		}
	}

	// Do some sanity checking before we start filtering things
	// Check the instance count of the node group matches the number of nodes found in Kubernetes
	if len(kubeNodes) != len(nodeGroupInstances) {
		nodesNotInCPNodeGroup, nodesNotInKube := findOffendingNodes(kubeNodes, nodeGroupInstances)
		var offendingNodesInfo string
		if len(nodesNotInCPNodeGroup) > 0 {
			offendingNodesInfo += "nodes not in node group: "
			offendingNodesInfo += strings.Join(nodesNotInCPNodeGroup, ",")
		}
		if len(nodesNotInKube) > 0 {
			if offendingNodesInfo != "" {
				offendingNodesInfo += ";"
			}
			offendingNodesInfo += "nodes not inside cluster: "
			offendingNodesInfo += strings.Join(nodesNotInKube, ",")
		}
		t.rm.LogEvent(t.cycleNodeRequest, "NodeCountMismatch",
			"node group: %v, kube: %v. %v",
			len(nodeGroupInstances), len(kubeNodes), offendingNodesInfo)

		// If it doesn't, then retry for a while in case something just scaled the node group
		timedOut, err := t.equilibriumWaitTimedOut()
		if err != nil {
			return t.transitionToHealing(err)
		}
		if timedOut {
			err := fmt.Errorf(
				"node count mismatch, number of kubernetes of nodes does not match number of cloud provider instances after %v",
				nodeEquilibriumWaitLimit)
			return t.transitionToHealing(err)
		}
		return reconcile.Result{Requeue: true, RequeueAfter: requeueDuration}, nil
	}

	// make a list of the nodes to terminate
	if len(t.cycleNodeRequest.Spec.NodeNames) > 0 {
		// If specific node names are provided, check they actually exist in the node group
		t.rm.LogEvent(t.cycleNodeRequest, "SelectingNodes", "Adding named nodes to NodesToTerminate")
		err := t.addNamedNodesToTerminate(kubeNodes, nodeGroupInstances)
		if err != nil {
			return t.transitionToHealing(err)
		}
	} else {
		// Otherwise just add all the nodes in the node group
		t.rm.LogEvent(t.cycleNodeRequest, "SelectingNodes", "Adding all node group nodes to NodesToTerminate")
		for _, kubeNode := range kubeNodes {
			// Check to ensure the kubeNode object maps to an existing node in the ASG
			// If this isn't the case, this is a phantom node. Fail the cnr to be safe.
			nodeGroupName, ok := nodeGroupInstances[kubeNode.Spec.ProviderID]
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
		if err = t.performInitialHealthChecks(kubeNodes); err != nil {
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
		t.rm.LogEvent(t.cycleNodeRequest, "ScalingUpWaiting", "Waiting for new nodes to be ready")
		return reconcile.Result{Requeue: true, RequeueAfter: requeueDuration}, nil
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
		var instanceFound bool = false

		for _, kubeNode := range kubeNodes {
			if node.Name == kubeNode.Name {
				instanceFound = true
				break
			}
		}

		if !instanceFound {
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
				t.cycleNodeRequest.Status.CurrentNodes = append(t.cycleNodeRequest.Status.CurrentNodes[:i], t.cycleNodeRequest.Status.CurrentNodes[i+1:]...)
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

	for _, node := range t.cycleNodeRequest.Status.CurrentNodes {
		if t.cycleNodeRequest.Spec.SkipPreTerminationChecks {
			t.rm.Logger.Info("Skipping pre-termination checks")
		}
		// Perform pre-termination checks before the node is cordoned
		// Cruicially, do this before the CNS is created for node to begin that process
		// The node should be ready for termination before any of this takes place
		if !t.cycleNodeRequest.Spec.SkipPreTerminationChecks && len(t.cycleNodeRequest.Spec.PreTerminationChecks) > 0 {
			// First try to send the trigger, if is has already been sent then this will
			// be skipped in the function. The trigger must only be sent once
			if err := t.sendPreTerminationTrigger(node); err != nil {
				return t.transitionToHealing(err)
			}

			// After the trigger has been sent, perform health checks to monitor if the node
			// can be cordoned/terminated. If all checks pass then it can be cordoned/terminated.
			allHealthChecksPassed, err := t.performPreTerminationHealthChecks(node)
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

		// Cordon the node and create a CycleNodeStatus CRD to do work on it
		if err := k8s.CordonNode(node.Name, t.rm.RawClient); err != nil {
			return t.transitionToHealing(err)
		}

		if err := t.rm.Client.Create(context.TODO(), t.makeCycleNodeStatusForNode(node.Name)); err != nil {
			return t.transitionToHealing(err)
		}

		// Add a label to the node to show that we've started working on it
		if err := k8s.AddLabelToNode(node.Name, cycleNodeLabel, t.cycleNodeRequest.Name, t.rm.RawClient); err != nil {
			t.rm.Logger.Error(err, "patch failed: could not add cyclops label to node", "nodeName", node.Name)
			return t.transitionToHealing(err)
		}
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

// transitionFailed handles failed CycleNodeRequests
func (t *CycleNodeRequestTransitioner) transitionHealing() (reconcile.Result, error) {
	nodeGroups, err := t.rm.CloudProvider.GetNodeGroups(t.cycleNodeRequest.GetNodeGroupNames())
	if err != nil {
		return t.transitionToFailed(err)
	}

	// try and re-attach the nodes, if any were un-attached
	for _, node := range t.cycleNodeRequest.Status.NodesToTerminate {
		t.rm.LogEvent(t.cycleNodeRequest, "AttachingNodes", "Attaching instances to nodes group: %v", node.Name)
		alreadyAttached, err := nodeGroups.AttachInstance(node.ProviderID, node.NodeGroupName)
		if alreadyAttached {
			continue
		}
		if err != nil {
			return t.transitionToFailed(err)
		}
	}

	// un-cordon after attach as well
	for _, node := range t.cycleNodeRequest.Status.NodesToTerminate {
		t.rm.LogEvent(t.cycleNodeRequest, "UncordoningNodes", "Uncordoning nodes in node group: %v", node.Name)
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return k8s.UncordonNode(node.Name, t.rm.RawClient)
		}); err != nil {
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

	// If deleting CycleNodeRequests is not enabled, stop here
	if !t.options.DeleteCNR {
		return reconcile.Result{}, nil
	}

	// Delete CycleNodeRequests that have reaped all of their children and are older
	// than the time configured to keep them for.
	if t.cycleNodeRequest.CreationTimestamp.Add(t.options.DeleteCNRExpiry).Before(time.Now()) {
		t.rm.Logger.Info("Deleting CycleNodeRequest")
		err := t.rm.Client.Delete(context.TODO(), t.cycleNodeRequest)
		if err != nil {
			t.rm.Logger.Error(err, "Unable to delete expired CycleNodeRequest")
		}
		return reconcile.Result{}, nil
	}

	// Requeue them for checking later if the expiry has been reached
	t.rm.Logger.Info("Requeuing CycleNodeRequest for deleting later")
	return reconcile.Result{Requeue: true, RequeueAfter: t.options.DeleteCNRRequeue}, nil
}

// transitionSkipped handles skipped CycleNodeRequests
func (t *CycleNodeRequestTransitioner) transitionSkipped() (reconcile.Result, error) {
	shouldRequeue, err := t.finalReapChildren()
	if err != nil {
		return t.transitionToHealing(err)
	}
	if shouldRequeue {
		return reconcile.Result{Requeue: true, RequeueAfter: transitionDuration}, nil
	}

	// If deleting CycleNodeRequests is not enabled, stop here
	if !t.options.DeleteCNR {
		return reconcile.Result{}, nil
	}

	// Delete CycleNodeRequests that have reaped all of their children and are older
	// than the time configured to keep them for.
	if t.cycleNodeRequest.CreationTimestamp.Add(t.options.DeleteCNRExpiry).Before(time.Now()) {
		t.rm.Logger.Info("Deleting CycleNodeRequest")
		err := t.rm.Client.Delete(context.TODO(), t.cycleNodeRequest)
		if err != nil {
			t.rm.Logger.Error(err, "Unable to delete expired CycleNodeRequest")
		}
		return reconcile.Result{}, nil
	}

	// Requeue them for checking later if the expiry has been reached
	t.rm.Logger.Info("Requeuing CycleNodeRequest for deleting later")
	return reconcile.Result{Requeue: true, RequeueAfter: t.options.DeleteCNRRequeue}, nil
}
