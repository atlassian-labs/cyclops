package transitioner

import (
	"fmt"

	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
)

// listReadyNodes lists nodes that are "ready". By default lists nodes that have also not been touched by Cyclops.
// A label is used to determine whether nodes have been touched by this CycleNodeRequest.
func (t *CycleNodeRequestTransitioner) listReadyNodes(includeInProgress bool) (map[string]corev1.Node, error) {
	nodes := make(map[string]corev1.Node)

	// Get the nodes
	selector, err := t.cycleNodeRequest.NodeLabelSelector()
	if err != nil {
		return nodes, err
	}

	nodeList, err := t.rm.ListNodes(selector)
	if err != nil {
		return nodes, err
	}

	// Filter the nodes down
	for _, node := range nodeList {
		if !includeInProgress {
			// Only add nodes that are not in progress
			if value, ok := node.Labels[cycleNodeLabel]; ok && value == t.cycleNodeRequest.Name {
				continue
			}
		}

		// Only add "Ready" nodes
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				nodes[node.Spec.ProviderID] = node
				break
			}
		}
	}

	return nodes, nil
}

// getNodesToTerminate returns a list of nodes that still need terminating and have not yet been actioned for
// this CycleNodeRequest.
// Also returns the number of nodes currently being cycled that still exist in the cluster.
func (t *CycleNodeRequestTransitioner) getNodesToTerminate(numNodes int64) (nodes []*corev1.Node, numNodesInProgress int, err error) {
	if numNodes < 0 {
		return nil, 0, fmt.Errorf("numNodes must be positive: got %d", numNodes)
	}

	// We have to include in progress nodes so we can count them
	kubeNodes, err := t.listReadyNodes(true)
	if err != nil {
		return nil, 0, err
	}

	for _, kubeNode := range kubeNodes {
		if value, ok := kubeNode.Labels[cycleNodeLabel]; ok && value == t.cycleNodeRequest.Name {
			numNodesInProgress++
		}
	}

	for _, nodeToTerminate := range t.cycleNodeRequest.Status.NodesToTerminate {
		kubeNode, found := kubeNodes[nodeToTerminate.ProviderID]

		if !found {
			continue
		}

		// Skip nodes that are already being worked on so we don't duplicate our work
		if value, ok := kubeNode.Labels[cycleNodeLabel]; ok && value == t.cycleNodeRequest.Name {
			continue
		}

		// Add nodes that need to be terminated but have not yet been actioned
		nodes = append(nodes, &kubeNode)

		for i := 0; i < len(t.cycleNodeRequest.Status.NodesAvailable); i++ {
			if kubeNode.Name == t.cycleNodeRequest.Status.NodesAvailable[i].Name {
				// Remove nodes from available if they are also scheduled for termination
				// Slice syntax removes this node at `i` from the array
				t.cycleNodeRequest.Status.NodesAvailable = append(
					t.cycleNodeRequest.Status.NodesAvailable[:i],
					t.cycleNodeRequest.Status.NodesAvailable[i+1:]...,
				)

				break
			}
		}

		// Stop finding nodes once we reach the desired amount
		if int64(len(nodes)) >= numNodes {
			break
		}
	}

	return nodes, numNodesInProgress, nil
}

// addNamedNodesToTerminate adds the named nodes for this CycleNodeRequest to the list of nodes to terminate.
// Skips any named node that does not exist in the node group for this CycleNodeRequest.
func (t *CycleNodeRequestTransitioner) addNamedNodesToTerminate(kubeNodes map[string]corev1.Node, nodeGroupInstances map[string]cloudprovider.Instance) error {
	nodeLookupByName := make(map[string]corev1.Node)

	for _, node := range kubeNodes {
		nodeLookupByName[node.Name] = node
	}

	for _, namedNode := range t.cycleNodeRequest.Spec.NodeNames {
		kubeNode, found := nodeLookupByName[namedNode]

		if !found {
			t.rm.Logger.Info("could not find node by name, skipping", "nodeName", namedNode)

			if !t.cycleNodeRequest.Spec.ValidationOptions.SkipMissingNamedNodes {
				return fmt.Errorf("could not find node by name: %v", namedNode)
			}

			t.rm.LogEvent(t.cycleNodeRequest, "SkipMissingNamedNode", "Named node %s not found", namedNode)
			continue
		}

		t.cycleNodeRequest.Status.NodesAvailable = append(
			t.cycleNodeRequest.Status.NodesAvailable,
			newCycleNodeRequestNode(&kubeNode, nodeGroupInstances[kubeNode.Spec.ProviderID].NodeGroupName()),
		)

		t.cycleNodeRequest.Status.NodesToTerminate = append(
			t.cycleNodeRequest.Status.NodesToTerminate,
			newCycleNodeRequestNode(&kubeNode, nodeGroupInstances[kubeNode.Spec.ProviderID].NodeGroupName()),
		)
	}

	return nil
}

// Find all the nodes in kube and the cloud provider that match the node selector and nodegroups
// specified in the CNR. These are two separate sets and the contents of one does not affect the
// contents of the other.
func (t *CycleNodeRequestTransitioner) findAllNodesForCycle() (kubeNodes map[string]corev1.Node, cloudProviderInstances map[string]cloudprovider.Instance, err error) {
	kubeNodes, err = t.listReadyNodes(true)
	if err != nil {
		return kubeNodes, cloudProviderInstances, err
	}

	if len(kubeNodes) == 0 {
		return kubeNodes, cloudProviderInstances, fmt.Errorf("no nodes matched selector")
	}

	// Only retain nodes which still exist inside cloud provider
	var nodeProviderIDs []string

	for _, node := range kubeNodes {
		nodeProviderIDs = append(nodeProviderIDs, node.Spec.ProviderID)
	}

	existingProviderIDs, err := t.rm.CloudProvider.InstancesExist(nodeProviderIDs)
	if err != nil {
		return kubeNodes, cloudProviderInstances, errors.Wrap(err, "failed to check instances that exist from cloud provider")
	}

	existingKubeNodes := make(map[string]corev1.Node)

	for _, validProviderID := range existingProviderIDs {
		if node, found := kubeNodes[validProviderID]; found {
			existingKubeNodes[node.Spec.ProviderID] = node
		}
	}

	kubeNodes = existingKubeNodes

	if len(kubeNodes) == 0 {
		return kubeNodes, cloudProviderInstances, fmt.Errorf("no existing nodes in cloud provider matched selector")
	}

	nodeGroupNames := t.cycleNodeRequest.GetNodeGroupNames()

	// Describe the node group for the request
	t.rm.LogEvent(t.cycleNodeRequest, "FetchingNodeGroup", "Fetching node group: %v", nodeGroupNames)

	if len(nodeGroupNames) == 0 {
		return kubeNodes, cloudProviderInstances, fmt.Errorf("must have at least one nodegroup name defined")
	}

	nodeGroups, err := t.rm.CloudProvider.GetNodeGroups(nodeGroupNames)
	if err != nil {
		return kubeNodes, cloudProviderInstances, err
	}

	return kubeNodes, nodeGroups.Instances(), nil
}

// newCycleNodeRequestNode converts a corev1.Node to a v1.CycleNodeRequestNode. This is done multiple
// times across the code, this function standardises the process
func newCycleNodeRequestNode(kubeNode *corev1.Node, nodeGroupName string) v1.CycleNodeRequestNode {
	var privateIP string

	// If there is no private IP, the error will be caught when trying
	// to perform a health check on the node
	for _, address := range kubeNode.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			privateIP = address.Address
		}
	}

	return v1.CycleNodeRequestNode{
		Name:          kubeNode.Name,
		ProviderID:    kubeNode.Spec.ProviderID,
		NodeGroupName: nodeGroupName,
		PrivateIP:     privateIP,
	}
}
