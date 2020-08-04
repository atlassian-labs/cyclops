package test

import (
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// NewTestPodWatcher creates a new test PodLister with given pods and options
func NewTestPodWatcher(pods []*v1.Pod, opts PodListerOptions) *podLister {
	store := cache.NewStore(cache.MetaNamespaceKeyFunc)
	for _, pod := range pods {
		_ = store.Add(pod)
	}
	return &podLister{store, opts}
}

// PodListerOptions for creating a new test PodLister
type PodListerOptions struct {
	ReturnErrorOnList bool
}

type podLister struct {
	store cache.Store
	opts  PodListerOptions
}

func (lister *podLister) List(selector labels.Selector) (ret []*v1.Pod, err error) {
	if lister.opts.ReturnErrorOnList {
		return ret, errors.New("unable to list pods")
	}
	err = cache.ListAll(lister.store, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.Pod))
	})
	return ret, err
}
