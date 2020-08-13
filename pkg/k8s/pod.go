package k8s

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("k8s.pod.go")

// EvictPod evicts a single pod from Kubernetes
func EvictPod(pod *v1.Pod, apiVersion string, client kubernetes.Interface) error {
	log.Info("Evicting pod", "podName", pod.Name, "nodeName", pod.Spec.NodeName, "apiVersion", apiVersion)
	return client.CoreV1().Pods(pod.Namespace).Evict(&v1beta1.Eviction{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: apiVersion,
			Kind:       evictionKind,
		},
		ObjectMeta:    pod.ObjectMeta,
		DeleteOptions: &metaV1.DeleteOptions{},
	})
}

// EvictPods evicts multiple pods from Kubernetes
func EvictPods(pods []*v1.Pod, apiVersion string, client kubernetes.Interface) (evictionErrors []error) {
	for _, pod := range pods {
		err := EvictPod(pod, apiVersion, client)
		if err != nil && !errors.IsNotFound(err) {
			evictionErrors = append(evictionErrors, err)
		}
	}
	return evictionErrors
}

// PodIsDaemonSet returns if the pod is a daemonset or not
func PodIsDaemonSet(pod *v1.Pod) bool {
	for _, ownerReference := range pod.ObjectMeta.OwnerReferences {
		if ownerReference.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// PodIsStatic returns if the pod is static or not
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
