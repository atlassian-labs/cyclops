package controller

import (
	"github.com/atlassian-labs/cyclops/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NewCyclopsManagedNodePredicate returns a predicate that passes only Node events
// where the node carries the Cyclops managed annotation.
func NewCyclopsManagedNodePredicate() predicate.TypedPredicate[*corev1.Node] {
	return cyclopsManagedNodePredicate{}
}

type cyclopsManagedNodePredicate struct{}

func (cyclopsManagedNodePredicate) Create(e event.TypedCreateEvent[*corev1.Node]) bool {
	return hasAnnotation(e.Object.GetAnnotations(), k8s.CyclopsManagedAnnotation)
}

func (cyclopsManagedNodePredicate) Update(e event.TypedUpdateEvent[*corev1.Node]) bool {
	return hasAnnotation(e.ObjectNew.GetAnnotations(), k8s.CyclopsManagedAnnotation)
}

func (cyclopsManagedNodePredicate) Delete(event.TypedDeleteEvent[*corev1.Node]) bool {
	return false
}

func (cyclopsManagedNodePredicate) Generic(e event.TypedGenericEvent[*corev1.Node]) bool {
	return hasAnnotation(e.Object.GetAnnotations(), k8s.CyclopsManagedAnnotation)
}

func hasAnnotation(annotations map[string]string, key string) bool {
	_, ok := annotations[key]
	return ok
}
