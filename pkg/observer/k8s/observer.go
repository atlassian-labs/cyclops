package k8s

import (
	"fmt"
	"strings"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/atlassian-labs/cyclops/pkg/observer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

// controllerRevisionLabel is the label key on pods for their daemonset controller hash
const controllerRevisionLabel = "controller-revision-hash"

// k8sObserver detects changes for OnDelete daemosnets
type k8sObserver struct {
	nodeLister      k8s.NodeLister
	podLister       k8s.PodLister
	daemonsetLister k8s.DaemonSetLister
	crLister        k8s.ControllerRevisionLister
}

// NewObserver creates the k8sObserver that detects changes on OnDelete daemosnets
func NewObserver(nodeLister k8s.NodeLister, podLister k8s.PodLister, daemonsetLister k8s.DaemonSetLister, crLister k8s.ControllerRevisionLister) observer.Observer {
	return &k8sObserver{
		nodeLister:      nodeLister,
		podLister:       podLister,
		daemonsetLister: daemonsetLister,
		crLister:        crLister,
	}
}

// collectRevisions maps revisions to their DaemonSets
// we list revisions by DS label as it automatically handles new revisions when DS labels have changed. k8s considers it a different controller
func collectRevisions(crLister k8s.ControllerRevisionLister, daemonsets map[string]*appsv1.DaemonSet) map[string][]*appsv1.ControllerRevision {
	collected := make(map[string][]*appsv1.ControllerRevision, len(daemonsets))

	for name, ds := range daemonsets {
		selector, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
		if err != nil {
			klog.Errorf("failed to parse selector %q for ds %q: %s", ds.Spec.Selector, name, err)
			continue
		}
		crs, err := crLister.List(selector)
		if err != nil {
			klog.Warningf("failed to list controller revisions %q for ds %q: %s", ds.Spec.Selector, name, err)
			continue
		}
		collected[name] = crs
		klog.V(7).Infoln("collected revisions", name, len(crs))
	}

	return collected
}

// collectPods maps pods to their DaemonSets names
// we need to get pods by OwnerReferences instead of labels in order to still observe out of date ones with the main DS labels
func collectPods(pods []*corev1.Pod, daemonsets map[string]*appsv1.DaemonSet) map[string][]*corev1.Pod {
	collected := make(map[string][]*corev1.Pod, len(daemonsets))

	for i, pod := range pods {
		var dsOwnerName string
		for _, owner := range pod.OwnerReferences {
			if owner.Kind != "DaemonSet" {
				continue
			}
			dsOwnerName = owner.Name
			break
		}

		if dsOwnerName == "" {
			// this pod doesn't belong to a DaemonSet
			continue
		}

		if _, ok := daemonsets[dsOwnerName]; !ok {
			// // this pod doesn't belong to a DaemonSet in our list
			continue
		}

		collected[dsOwnerName] = append(collected[dsOwnerName], pods[i])
	}

	return collected
}

// maxRevision returns the max revision number of the given list of histories
func maxRevision(revisions []*appsv1.ControllerRevision) *appsv1.ControllerRevision {
	var max int64
	var maxIndex int
	for i, revision := range revisions {
		if revision.Revision > max {
			max = revision.Revision
			maxIndex = i
		}
	}
	return revisions[maxIndex]
}

// podOutOfDate returns if the pod is out of date compared to the max ControllerRevision in the list
func (c *k8sObserver) podOutOfDate(pod *corev1.Pod, revisions []*appsv1.ControllerRevision) (bool, string) {
	crPodHash, ok := pod.Labels[controllerRevisionLabel]
	if !ok {
		reason := fmt.Sprintf("no controller revision label %q for pod %q", controllerRevisionLabel, pod.Name)
		klog.Warningln(reason)
		return false, reason
	}

	maxRev := maxRevision(revisions)
	crMaxHash, ok := maxRev.Labels[controllerRevisionLabel]
	if !ok {
		reason := fmt.Sprintf("no controller revision label %q for controller revision %q", controllerRevisionLabel, maxRev.Name)
		klog.Warningln(reason)
		return false, reason
	}

	if crPodHash == crMaxHash {
		return false, fmt.Sprintf("pod %q hash %q is up to date with latest daemonset controller revision %q rev %d", pod.Name, crPodHash, maxRev.Name, maxRev.Revision)
	}

	return true, fmt.Sprintf("pod %q hash %q is not up to date with latest daemonset controller revision %q hash %q rev %d", pod.Name, crPodHash, maxRev.Name, crMaxHash, maxRev.Revision)
}

// Changed returns the nodegroups and nodes which changed because of an out of date pod from it's OnDelete DaemonSet
func (c *k8sObserver) Changed(nodeGroups *atlassianv1.NodeGroupList) []*observer.ListedNodeGroups {
	var changedNodeGroups []*observer.ListedNodeGroups

	if len(nodeGroups.Items) == 0 {
		klog.V(4).Infoln("no nodegroups to check")
		return nil
	}

	// pre list and index as much as we can
	daemonsets, err := c.daemonsetLister.List(labels.Everything())
	if err != nil {
		klog.Errorln("failed to list daemonsets:", err)
		return nil
	}
	indexedDaemonsets := make(map[string]*appsv1.DaemonSet, len(daemonsets))
	for i, ds := range daemonsets {
		if ds.Spec.UpdateStrategy.Type != appsv1.OnDeleteDaemonSetStrategyType {
			klog.V(5).Infof("daemonset %q is not OnDelete: skipping", ds.Name)
			continue
		}
		indexedDaemonsets[ds.Name] = daemonsets[i]
	}

	pods, err := c.podLister.List(labels.Everything())
	if err != nil {
		klog.Errorln("failed to list pods:", err)
		return nil
	}

	// over every node group work out which pods are out of date
	for nodeGroupIndex, nodeGroup := range nodeGroups.Items {
		klog.V(4).Infoln("k8s observer: checking nodegroup", nodeGroup.Name)

		// get nodes
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
		indexedNodes := make(map[string]*corev1.Node, len(nodes))
		for i, n := range nodes {
			indexedNodes[n.Name] = nodes[i]
		}

		// filter pods by node for this nodegroup
		var filteredPods []*corev1.Pod
		for i, pod := range pods {
			if _, ok := indexedNodes[pod.Spec.NodeName]; ok {
				filteredPods = append(filteredPods, pods[i])
				continue
			}
		}

		// map pods and revisions to their daemonsets
		collectedPods := collectPods(filteredPods, indexedDaemonsets)
		collectedRevisions := collectRevisions(c.crLister, indexedDaemonsets)

		// for each daemonset, check any of it's pods are out of date
		changedNodes := map[string]*corev1.Node{}
		changedReasons := map[string][]string{}
		for dsName, pods := range collectedPods {
			for _, pod := range pods {
				// check if the node for the pod is already known to be out of date, if it is we don't need to check it again
				if _, ok := changedNodes[pod.Spec.NodeName]; ok {
					klog.V(5).Infof("node %q already out of date: skipping pod %q", pod.Spec.NodeName, pod.Name)
					continue
				}

				// don't care about pods that aren't running, in case a bad config is deploy don't cycle
				if pod.Status.Phase != corev1.PodRunning {
					klog.V(4).Infof("pod %q not Running (current status %s): skipping", pod.Name, pod.Status.Phase)
					continue
				}

				revisions, ok := collectedRevisions[dsName]
				if !ok {
					klog.Warningln("no revisions found for daemonset", dsName)
					continue
				}

				outOfDate, reason := c.podOutOfDate(pod, revisions)
				if !outOfDate {
					klog.V(4).Infof("[OK] %s: %s", dsName, reason)
					continue
				}

				// add the the index changed map for this nodegroup
				klog.V(4).Infof("[OUT OF DATE] %s: %s", dsName, reason)
				changedNodes[pod.Spec.NodeName] = indexedNodes[pod.Spec.NodeName]
				changedReasons[nodeGroup.Name] = append(changedReasons[nodeGroup.Name], reason)
			}
		}

		if len(changedNodes) > 0 {
			// convert to a list of nodes to add to the ListedNodeGroup
			changedNodesList := make([]*corev1.Node, 0, len(changedNodes))
			for name := range changedNodes {
				changedNodesList = append(changedNodesList, changedNodes[name])
			}

			changedNodeGroups = append(changedNodeGroups, &observer.ListedNodeGroups{
				NodeGroup: &nodeGroups.Items[nodeGroupIndex],
				List:      changedNodesList,
				Reason:    strings.Join(changedReasons[nodeGroup.Name], "\n"),
			})
		}
	}

	return changedNodeGroups
}
