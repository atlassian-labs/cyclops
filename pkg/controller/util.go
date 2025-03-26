package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type namespacePredicate[object client.Object] struct {
	namespace string
}

// NewNamespacePredicate returns a filtering predicate that will filter out events that do not
// belong to the provided namespace.
func NewNamespacePredicate[object client.Object](namespace string) predicate.TypedPredicate[object] {
	return &namespacePredicate[object]{
		namespace: namespace,
	}
}

func (n *namespacePredicate[object]) Create(e event.TypedCreateEvent[object]) bool {
	return e.Object.GetNamespace() == n.namespace
}

func (n *namespacePredicate[object]) Delete(e event.TypedDeleteEvent[object]) bool {
	return e.Object.GetNamespace() == n.namespace
}

func (n *namespacePredicate[object]) Update(e event.TypedUpdateEvent[object]) bool {
	return e.ObjectNew.GetNamespace() == n.namespace
}

func (n *namespacePredicate[object]) Generic(e event.TypedGenericEvent[object]) bool {
	return e.Object.GetNamespace() == n.namespace
}
