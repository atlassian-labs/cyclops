package k8s

import (
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// DaemonSetLister defines a type that can list DaemonSets with a selector
type DaemonSetLister interface {
	List(labels.Selector) ([]*v1.DaemonSet, error)
}

// cachedDaemonSetList lists from multiple caches
type cachedDaemonSetList struct {
	caches []cache.Indexer
}

// NewCachedDaemonSetList creates a new cachedDaemonSetList
func NewCachedDaemonSetList(caches ...cache.Indexer) DaemonSetLister {
	return &cachedDaemonSetList{caches: caches}
}

// List daemonsets from multiple caches using the label Selector
func (c *cachedDaemonSetList) List(selector labels.Selector) ([]*v1.DaemonSet, error) {
	var dsets []*v1.DaemonSet

	for _, ca := range c.caches {
		err := cache.ListAll(ca, selector, func(v interface{}) {
			if d, ok := v.(*v1.DaemonSet); ok {
				dsets = append(dsets, d)
			}
		})
		if err != nil {
			return dsets, err
		}
	}

	return dsets, nil
}

// ControllerRevisionLister defines a type that can list ControllerRevision with a selector
type ControllerRevisionLister interface {
	List(labels.Selector) ([]*v1.ControllerRevision, error)
}

// cachedControllerRevisionList lists from multiple caches
type cachedControllerRevisionList struct {
	caches []cache.Indexer
}

// NewCachedControllerRevisionList creates a new cachedControllerRevisionList
func NewCachedControllerRevisionList(caches ...cache.Indexer) ControllerRevisionLister {
	return &cachedControllerRevisionList{caches: caches}
}

// List ControllerRevision from multiple caches using the label Selector
func (c *cachedControllerRevisionList) List(selector labels.Selector) ([]*v1.ControllerRevision, error) {
	var dsets []*v1.ControllerRevision

	for _, ca := range c.caches {
		err := cache.ListAll(ca, selector, func(v interface{}) {
			if d, ok := v.(*v1.ControllerRevision); ok {
				dsets = append(dsets, d)
			}
		})
		if err != nil {
			return dsets, err
		}
	}

	return dsets, nil
}
