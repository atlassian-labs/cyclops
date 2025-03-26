package k8s

import (
	"time"

	"k8s.io/klog/v2"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const resyncPeriod = 30 * time.Minute

// Watch returns a controller that will watch the specified resource
func Watch(c cache.Getter, resource string, namespace string, resourceEventHandler cache.ResourceEventHandler, objType runtime.Object, fieldSelector fields.Selector) (cache.Indexer, cache.Controller) {
	lw := cache.NewListWatchFromClient(c, resource, namespace, fieldSelector)
	store, controller := cache.NewInformerWithOptions(
		cache.InformerOptions{
			ListerWatcher:   lw,
			ObjectType:      objType,
			Handler:         resourceEventHandler,
			ResyncPeriod:    resyncPeriod,
			Indexers:        cache.Indexers{},
		},
	)
	indexer, ok := store.(cache.Indexer)
	if !ok {
		panic("expected Indexer, but got a Store that does not implement Indexer")
	}
	return indexer, controller
}

// WatchResourceFunc defines a function that can set up a cache and controller
type WatchResourceFunc func(kubernetes.Interface, string, cache.ResourceEventHandler) (cache.Indexer, cache.Controller)

// WatchNodes watches all nodes
func WatchNodes(client kubernetes.Interface, _ string, resourceEventHandler cache.ResourceEventHandler) (cache.Indexer, cache.Controller) {
	return Watch(client.CoreV1().RESTClient(), "nodes", metav1.NamespaceNone, resourceEventHandler, new(corev1.Node), fields.Everything())
}

// WatchPods watches all pods
func WatchPods(client kubernetes.Interface, namespace string, resourceEventHandler cache.ResourceEventHandler) (cache.Indexer, cache.Controller) {
	return Watch(client.CoreV1().RESTClient(), "pods", namespace, resourceEventHandler, new(corev1.Pod), fields.Everything())
}

// WatchDaemonSets watches all daemonsets
func WatchDaemonSets(client kubernetes.Interface, namespace string, resourceEventHandler cache.ResourceEventHandler) (cache.Indexer, cache.Controller) {
	return Watch(client.AppsV1().RESTClient(), "daemonsets", namespace, resourceEventHandler, new(appsv1.DaemonSet), fields.Everything())
}

// WatchControllerRevisions watches all daemonsets
func WatchControllerRevisions(client kubernetes.Interface, namespace string, resourceEventHandler cache.ResourceEventHandler) (cache.Indexer, cache.Controller) {
	return Watch(client.AppsV1().RESTClient(), "controllerrevisions", namespace, resourceEventHandler, new(appsv1.ControllerRevision), fields.Everything())
}

// StartWatching starts watching with the watchFn and returns the cache to query from
func StartWatching(client kubernetes.Interface, namespace string, watchFn WatchResourceFunc, stopCh <-chan struct{}) cache.Indexer {
	// no event handling needed for this paradigm, leave default
	indexer, watcher := watchFn(client, namespace, cache.ResourceEventHandlerFuncs{})
	go watcher.Run(stopCh)
	for {
		if watcher.HasSynced() {
			klog.V(4).Infoln("watcher synced: ", indexer.ListKeys())
			break
		}
	}
	return indexer
}
