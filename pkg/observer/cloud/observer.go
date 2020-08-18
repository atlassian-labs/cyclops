package cloud

import (
	"fmt"
	"strings"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/atlassian-labs/cyclops/pkg/observer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// cloudObserver is an observer that detects changes in cloud provider instances from their ASG configuration
type cloudObserver struct {
	cloudProvider cloudprovider.CloudProvider
	nodeLister    k8s.NodeLister
}

// NewObserver creates an observer that detects changes in cloud provider instances from their ASG configuration
func NewObserver(cloudProvider cloudprovider.CloudProvider, nodeLister k8s.NodeLister) observer.Observer {
	return &cloudObserver{cloudProvider: cloudProvider, nodeLister: nodeLister}
}

// Changed detects changes on cloud provider nodes
func (c *cloudObserver) Changed(nodeGroups *atlassianv1.NodeGroupList) []*observer.ListedNodeGroups {
	var changed []*observer.ListedNodeGroups

	for i, nodeGroup := range nodeGroups.Items {
		klog.V(4).Infoln("cloud observer: checking nodegroup", nodeGroup.Name)

		// fetch the cloud provider node group and the nodes for that node group
		cloudNodeGroups, err := c.cloudProvider.GetNodeGroups(nodeGroup.GetNodeGroupNames())
		if err != nil {
			klog.Errorln("could not find cloud provider nodegroups named", nodeGroup.GetNodeGroupNames())
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(&nodeGroup.Spec.NodeSelector)
		if err != nil {
			klog.Errorf("failed to parse selector %q for nodegroup %q: %s", nodeGroup.Spec.NodeSelector, nodeGroup.Name, err)
			continue
		}
		nodes, err := c.nodeLister.List(selector)
		if err != nil {
			klog.Errorf("failed to list nodes for nodegroup %q: %s", nodeGroup.Name, err)
			continue
		}

		// check if out of date and match to k8s node
		var outOfDateNodes []*corev1.Node
		var outOfDateInstanceReasons []string
		for _, instance := range cloudNodeGroups.ReadyInstances() {
			for _, node := range nodes {
				if instance.MatchesProviderID(node.Spec.ProviderID) {
					if instance.OutOfDate() {
						reason := fmt.Sprintf("instance %q / node %q not up to date with current cloud provider node group configuration/template", instance.ID(), node.Name)
						klog.V(4).Infof("[OUT OF DATE] %s", reason)
						outOfDateInstanceReasons = append(outOfDateInstanceReasons, reason)
						outOfDateNodes = append(outOfDateNodes, node)
					} else {
						klog.V(5).Infof("[OK] instance %s is up to date", instance.ID())
					}
					break
				}
			}
		}

		if len(outOfDateNodes) > 0 {
			outOfDateNodesCopy := make([]*corev1.Node, len(outOfDateNodes))
			copy(outOfDateNodesCopy, outOfDateNodes)

			changed = append(changed, &observer.ListedNodeGroups{
				NodeGroup: &nodeGroups.Items[i],
				List:      outOfDateNodesCopy,
				Reason:    strings.Join(outOfDateInstanceReasons, "\n"),
			})
		}
	}

	return changed
}
