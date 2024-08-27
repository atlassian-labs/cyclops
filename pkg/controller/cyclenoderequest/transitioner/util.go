package transitioner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// findValidNodes performs an AND operation on the two sets of nodes. It finds all the nodes
// in both kube and the cloud provider nodegroups. This is considered the valid set of nodes
// that can be operated on.
func findValidNodes(kubeNodes map[string]corev1.Node, nodeGroupInstances map[string]cloudprovider.Instance) (map[string]corev1.Node, map[string]cloudprovider.Instance) {
	validKubeNodes := make(map[string]corev1.Node)
	validNodegroupInstances := make(map[string]cloudprovider.Instance)

	for providerId, kubeNode := range kubeNodes {
		if _, exists := nodeGroupInstances[providerId]; exists {
			validKubeNodes[providerId] = kubeNode
		}
	}

	for providerId, nodeGroupInstance := range nodeGroupInstances {
		if _, exists := nodeGroupInstances[providerId]; exists {
			validNodegroupInstances[providerId] = nodeGroupInstance
		}
	}

	return validKubeNodes, validNodegroupInstances
}

// findProblemNodes performs an XOR operation on the two sets of nodes. It finds all the nodes
// in either kube or the cloud provider nodegroups, but not both. These are considered the
// problems sets of nodes that need to be dealt with before cycling can occur.
func findProblemNodes(kubeNodes map[string]corev1.Node, nodeGroupInstances map[string]cloudprovider.Instance) (map[string]corev1.Node, map[string]cloudprovider.Instance) {
	problemKubeNodes := make(map[string]corev1.Node)
	problemNodegroupInstances := make(map[string]cloudprovider.Instance)

	for providerId, kubeNode := range kubeNodes {
		if _, exists := nodeGroupInstances[providerId]; !exists {
			problemKubeNodes[providerId] = kubeNode
		}
	}

	for providerId, nodeGroupInstance := range nodeGroupInstances {
		if _, exists := kubeNodes[providerId]; !exists {
			problemNodegroupInstances[providerId] = nodeGroupInstance
		}
	}

	return problemKubeNodes, problemNodegroupInstances
}

// reattachAnyDetachedInstances re-attaches any instances which are detached from the cloud
// provider nodegroups defined in the CNR using the cycling annotation to identify which one.
func (t *CycleNodeRequestTransitioner) reattachAnyDetachedInstances(nodesNotInCloudProviderNodegroup map[string]corev1.Node) error {
	var nodeProviderIDs []string

	if len(nodesNotInCloudProviderNodegroup) == 0 {
		return nil
	}

	for _, node := range nodesNotInCloudProviderNodegroup {
		nodeProviderIDs = append(nodeProviderIDs, node.Spec.ProviderID)
	}

	existingProviderIDs, err := t.rm.CloudProvider.InstancesExist(nodeProviderIDs)
	if err != nil {
		return errors.Wrap(err, "failed to check instances that exist from cloud provider")
	}

	nodeGroups, err := t.rm.CloudProvider.GetNodeGroups(t.cycleNodeRequest.GetNodeGroupNames())
	if err != nil {
		return err
	}

	for providerID, node := range nodesNotInCloudProviderNodegroup {
		_, instanceExists := existingProviderIDs[providerID]

		if !instanceExists {
			continue
		}

		// The kube node is now established to be backed by an instance in the cloud provider
		// that is detached from it's nodegroup. Use the nodegroup annotation from the kube
		// node set as part of a previous cycle to re-attach it.
		nodegroupName, err := t.rm.GetNodegroupFromNodeAnnotation(node.Name)

		// If there is an error because the kube node no longer exists then simply skip any
		// more action on the node and go to the next one. This case can be fixed in the next
		// run of the Pending phase.
		if apierrors.IsNotFound(err) {
			continue
		}

		// Otherwise error out to end the cycle. This includes if the cycling annotation is
		// missing since there is no link between the original cloud provider instance and it's
		// nodegroup so there's no way to re-attach it.
		if err != nil {
			return err
		}

		// AttachInstance does not error out if the instance does not exist so no need to handle
		// it here. Error out on any error that can't be fixed by repeating the attempts to fix
		// the instance state.
		alreadyAttached, err := nodeGroups.AttachInstance(node.Spec.ProviderID, nodegroupName)
		if err != nil && !alreadyAttached {
			return err
		}
	}

	return nil
}

// deleteAnyOrphanedKubeNodes filters through the kube nodes without instances in the cloud provider
// nodegroups which don't have an instance in the cloud provider at all. It removes the cycling
// finalizer and deletes the node.
func (t *CycleNodeRequestTransitioner) deleteAnyOrphanedKubeNodes(nodesNotInCloudProviderNodegroup map[string]corev1.Node) error {
	var nodeProviderIDs []string

	if len(nodesNotInCloudProviderNodegroup) == 0 {
		return nil
	}

	for _, node := range nodesNotInCloudProviderNodegroup {
		nodeProviderIDs = append(nodeProviderIDs, node.Spec.ProviderID)
	}

	existingProviderIDs, err := t.rm.CloudProvider.InstancesExist(nodeProviderIDs)
	if err != nil {
		return errors.Wrap(err, "failed to check instances that exist from cloud provider")
	}

	// Find all the orphaned kube nodes from the set of nodes without matching instance in the
	// cloud provider nodegroup.
	for providerID, node := range nodesNotInCloudProviderNodegroup {
		_, instanceExists := existingProviderIDs[providerID]

		if instanceExists {
			continue
		}

		// The kube node is now established to be orphaned. Check the finalizers on the node
		// object and ensure that only the Cyclops finalizer exists on it. If another finalizer
		// exists then it's from another controller and it should not be deleted.
		containsNonCyclingFinalizer, err := t.rm.NodeContainsNonCyclingFinalizer(node.Name)
		if err != nil {
			return errors.Wrap(err,
				fmt.Sprintf("failed to check if node %s contains a non-cycling finalizer", node.Name),
			)
		}

		if containsNonCyclingFinalizer {
			return fmt.Errorf("can't delete node %s because it contains non-cycling finalizers: %v",
				node.Name,
				node.Finalizers,
			)
		}

		// The cycling finalizer is the only one on the node so remove it.
		if err := t.rm.RemoveFinalizerFromNode(node.Name); err != nil {
			return err
		}

		// Delete the node to ensure it gets removed from kube. It is possible that the finalizer
		// was preventing the node object from being deleted and the node is deleted by the time
		// this delete call is reached. Don't if the node is already deleted since that is desired
		// effect.
		if err := t.rm.DeleteNode(node.Name); err != nil {
			return err
		}
	}

	return nil
}

// errorIfEquilibriumTimeoutReached reduces the footprint of this check in the
// Pending transition
func (t *CycleNodeRequestTransitioner) errorIfEquilibriumTimeoutReached() error {
	timedOut, err := t.equilibriumWaitTimedOut()
	if err != nil {
		return err
	}

	if timedOut {
		return fmt.Errorf(
			"node count mismatch, number of kubernetes nodes does not match number of cloud provider instances after %v",
			nodeEquilibriumWaitLimit,
		)
	}

	return nil
}

// logProblemNodes generates event message describing any issues with the node state prior
// to cycling.
func (t *CycleNodeRequestTransitioner) logProblemNodes(nodesNotInCloudProviderNodegroup map[string]corev1.Node, instancesNotInKube map[string]cloudprovider.Instance) {
	var offendingNodesInfo string

	if len(nodesNotInCloudProviderNodegroup) > 0 {
		providerIDs := make([]string, 0)

		for providerID := range nodesNotInCloudProviderNodegroup {
			providerIDs = append(providerIDs,
				fmt.Sprintf("id %q", providerID),
			)
		}

		offendingNodesInfo += "nodes not in node group: "
		offendingNodesInfo += strings.Join(providerIDs, ",")
	}

	if len(instancesNotInKube) > 0 {
		if offendingNodesInfo != "" {
			offendingNodesInfo += ";"
		}

		providerIDs := make([]string, 0)

		for providerID, node := range instancesNotInKube {
			providerIDs = append(providerIDs,
				fmt.Sprintf("id %q in %q", providerID, node.NodeGroupName()),
			)
		}

		offendingNodesInfo += "nodes not inside cluster: "
		offendingNodesInfo += strings.Join(providerIDs, ",")
	}

	message := fmt.Sprintf(
		"instances missing from cloud provider nodegroup: %v, kube nodes missing: %v. %v",
		len(nodesNotInCloudProviderNodegroup),
		len(instancesNotInKube),
		offendingNodesInfo,
	)

	// Send to both so because this is important info that needs to be found
	// more easily
	t.rm.Logger.Info(message)
	t.rm.LogEvent(t.cycleNodeRequest, "NodeStateInvalid", message)
}

// validateInstanceState performs final validation on the nodegroup to ensure
// that all the cloud provider instances are ready in the nodegroup.
func (t *CycleNodeRequestTransitioner) validateInstanceState(validNodeGroupInstances map[string]cloudprovider.Instance) (bool, error) {
	nodeGroups, err := t.rm.CloudProvider.GetNodeGroups(
		t.cycleNodeRequest.GetNodeGroupNames(),
	)

	if err != nil {
		return false, err
	}

	if len(nodeGroups.ReadyInstances()) == len(validNodeGroupInstances) {
		return true, nil
	}

	return false, nil
}

// deleteFailedSiblingCNRs finds the CNRs generated for the same nodegroup as
// the one in the calling transitioner. It filters for deleted CNRs in the same
// namespace and deletes them.
func (t *CycleNodeRequestTransitioner) deleteFailedSiblingCNRs() error {
	ctx := context.TODO()

	var list v1.CycleNodeRequestList

	err := t.rm.Client.List(ctx, &list, &client.ListOptions{
		Namespace: t.cycleNodeRequest.Namespace,
	})

	if err != nil {
		return err
	}

	for _, cnr := range list.Items {
		// Filter out CNRs generated for another Nodegroup
		if !t.cycleNodeRequest.IsFromSameNodeGroup(cnr) {
			continue
		}

		// Filter out CNRs not in the Failed phase
		if cnr.Status.Phase != v1.CycleNodeRequestFailed {
			continue
		}

		if err := t.rm.Client.Delete(ctx, &cnr); err != nil {
			return err
		}
	}

	return nil
}
