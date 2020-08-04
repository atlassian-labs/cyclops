package test

import (
	"errors"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// NewTestNodeWatcher creates a new mock NodeLister with the given nodes and options
func NewTestNodeWatcher(nodes []*v1.Node, opts NodeListerOptions) *nodeLister {
	store := cache.NewStore(cache.MetaNamespaceKeyFunc)
	for _, node := range nodes {
		_ = store.Add(node)
	}
	return &nodeLister{store, opts}
}

// NodeListerOptions options for creating test NodeLister
type NodeListerOptions struct {
	ReturnErrorOnList bool
}

type nodeLister struct {
	store cache.Store
	opts  NodeListerOptions
}

func (lister *nodeLister) List(selector labels.Selector) (ret []*v1.Node, err error) {
	if lister.opts.ReturnErrorOnList {
		return ret, errors.New("unable to list nodes")
	}
	err = cache.ListAll(lister.store, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.Node))
	})
	return ret, err
}
