package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type namespacePredicate struct {
	namespace string
}

// NewNamespacePredicate returns a filtering predicate that will filter out events that do not
// belong to the provided namespace.
func NewNamespacePredicate(namespace string) predicate.Predicate {
	return &namespacePredicate{namespace: namespace}
}

func (n *namespacePredicate) Create(e event.CreateEvent) bool {
	return e.Meta.GetNamespace() == n.namespace
}

func (n *namespacePredicate) Delete(e event.DeleteEvent) bool {
	return e.Meta.GetNamespace() == n.namespace
}

func (n *namespacePredicate) Update(e event.UpdateEvent) bool {
	return e.MetaNew.GetNamespace() == n.namespace
}

func (n *namespacePredicate) Generic(e event.GenericEvent) bool {
	return e.Meta.GetNamespace() == n.namespace
}
