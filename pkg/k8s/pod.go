package k8s

import (
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	podConditionTypeForUnhealthy = v1.PodReady
)

var log = logf.Log.WithName("k8s.pod.go")

// ForciblyDeletePod immediately terminates a given pod off the node, without waiting to check if it has been removed.
// This should only be called on workloads that can tolerate being removed like this, and as a last resort.
func ForciblyDeletePod(podName, podNamespace, nodeName string, client kubernetes.Interface) error {
	log.Info("Forcibly deleting pod", "podName", podName,
		"podNamespace", podNamespace, "nodeName", nodeName)
	var gracePeriodDeleteNow int64 = 0
	return client.CoreV1().Pods(podNamespace).Delete(context.TODO(), podName, metaV1.DeleteOptions{
		GracePeriodSeconds: &gracePeriodDeleteNow,
	})
}

// EvictPod evicts a single pod from a Kubernetes node
func EvictPod(pod *v1.Pod, apiVersion string, client kubernetes.Interface) error {
	log.Info("Evicting pod", "podName", pod.Name, "podNamespace", pod.Namespace,
		"nodeName", pod.Spec.NodeName, "apiVersion", apiVersion)
	return client.CoreV1().Pods(pod.Namespace).Evict(context.TODO(), &v1beta1.Eviction{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: apiVersion,
			Kind:       evictionKind,
		},
		ObjectMeta:    pod.ObjectMeta,
		DeleteOptions: &metaV1.DeleteOptions{},
	})
}

// EvictOrForciblyDeletePod tries to evict a pod, and if that fails will then check if it can forcibly remove the pod instead.
func EvictOrForciblyDeletePod(pod *v1.Pod, apiVersion string, client kubernetes.Interface, unhealthyAfter time.Duration, now time.Time) error {
	err := EvictPod(pod, apiVersion, client)
	if err != nil {
		// If we couldn't drain the pod, double check if it's been unhealthy for too long and if it has then
		// force it off the node so we can continue.
		if serr, ok := err.(*errors.StatusError); ok && errors.IsTooManyRequests(serr) {
			if PodIsLongtermUnhealthy(pod.Status, unhealthyAfter, now) {
				log.Info("Pod is un-evictable and is unhealthy for longer than the unhealthy threshold",
					"podName", pod.Name, "podNamespace", pod.Namespace, "nodeName", pod.Spec.NodeName,
					"unhealthyThreshold", unhealthyAfter)
				return ForciblyDeletePod(pod.Name, pod.Namespace, pod.Spec.NodeName, client)
			}
		} else {
			return err
		}
	}
	return nil
}

// EvictPods evicts multiple pods from a Kubernetes node. Forcibly removes a pod if it is old and unhealthy and
// stopping the eviction as a result.
func EvictPods(pods []*v1.Pod, apiVersion string, client kubernetes.Interface, unhealthyAfter time.Duration, now time.Time) (evictionErrors []error) {
	for _, pod := range pods {
		err := EvictOrForciblyDeletePod(pod, apiVersion, client, unhealthyAfter, now)
		if err != nil && !errors.IsNotFound(err) {
			evictionErrors = append(evictionErrors, err)
		}
	}
	return evictionErrors
}

// PodIsDaemonSet returns true if the pod is a daemonset
func PodIsDaemonSet(pod *v1.Pod) bool {
	for _, ownerReference := range pod.ObjectMeta.OwnerReferences {
		if ownerReference.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// PodIsLongtermUnhealthy returns true if the pod has had container startup or restarting issues
// for a period of time
func PodIsLongtermUnhealthy(podStatus v1.PodStatus, unhealthyAfter time.Duration, now time.Time) bool {
	// If the pod isn't ready (i.e. all containers are ready at the same time AND able to serve traffic) for a given
	// time period then mark it unhealthy.
	for _, cond := range podStatus.Conditions {
		if cond.Type == podConditionTypeForUnhealthy && cond.Status == v1.ConditionFalse {
			timeThresholdForUnhealthy := metaV1.NewTime(now.UTC().Add(-unhealthyAfter))
			if cond.LastTransitionTime.Before(&timeThresholdForUnhealthy) {
				return true
			}
		}
	}
	return false
}

// PodIsStatic returns true if the pod is static
func PodIsStatic(pod *v1.Pod) bool {
	configSource, ok := pod.ObjectMeta.Annotations["kubernetes.io/config.source"]
	return ok && configSource == "file"
}

// PodLister defines a type that can list pods with a label selector
type PodLister interface {
	List(labels.Selector) ([]*v1.Pod, error)
}

// cachedPodList defines a type that can list pods from multiple caches
type cachedPodList struct {
	caches []cache.Indexer
}

// NewCachedPodList creates a new cachedPodList
func NewCachedPodList(caches ...cache.Indexer) PodLister {
	return &cachedPodList{caches: caches}
}

// List pods with a label selector across multiple caches
func (c *cachedPodList) List(selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod

	for _, ca := range c.caches {
		err := cache.ListAll(ca, selector, func(v interface{}) {
			if p, ok := v.(*v1.Pod); ok {
				pods = append(pods, p)
			}
		})
		if err != nil {
			return pods, err
		}
	}

	return pods, nil
}
