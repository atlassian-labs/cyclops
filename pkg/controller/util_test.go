package controller

import (
	"testing"

	"github.com/atlassian-labs/cyclops/pkg/k8s"
	"github.com/stretchr/testify/assert"
	coreV1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func testPredicateNode(name string, annotations map[string]string) *coreV1.Node {
	return &coreV1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
	}
}

func TestCyclopsManagedNodePredicate(t *testing.T) {
	p := NewCyclopsManagedNodePredicate()

	t.Run("Create with annotation", func(t *testing.T) {
		e := event.TypedCreateEvent[*coreV1.Node]{
			Object: testPredicateNode("n", map[string]string{k8s.CyclopsManagedAnnotation: "true"}),
		}
		assert.True(t, p.Create(e))
	})

	t.Run("Create without annotation", func(t *testing.T) {
		e := event.TypedCreateEvent[*coreV1.Node]{
			Object: testPredicateNode("n", nil),
		}
		assert.False(t, p.Create(e))
	})

	t.Run("Update new has annotation", func(t *testing.T) {
		e := event.TypedUpdateEvent[*coreV1.Node]{
			ObjectOld: testPredicateNode("n", nil),
			ObjectNew: testPredicateNode("n", map[string]string{k8s.CyclopsManagedAnnotation: "true"}),
		}
		assert.True(t, p.Update(e))
	})

	t.Run("Update new has no annotation", func(t *testing.T) {
		e := event.TypedUpdateEvent[*coreV1.Node]{
			ObjectOld: testPredicateNode("n", map[string]string{k8s.CyclopsManagedAnnotation: "true"}),
			ObjectNew: testPredicateNode("n", nil),
		}
		assert.False(t, p.Update(e))
	})

	t.Run("Delete always false", func(t *testing.T) {
		e := event.TypedDeleteEvent[*coreV1.Node]{
			Object: testPredicateNode("n", map[string]string{k8s.CyclopsManagedAnnotation: "true"}),
		}
		assert.False(t, p.Delete(e))
	})

	t.Run("Generic with annotation", func(t *testing.T) {
		e := event.TypedGenericEvent[*coreV1.Node]{
			Object: testPredicateNode("n", map[string]string{k8s.CyclopsManagedAnnotation: "true"}),
		}
		assert.True(t, p.Generic(e))
	})

	t.Run("Generic without annotation", func(t *testing.T) {
		e := event.TypedGenericEvent[*coreV1.Node]{
			Object: testPredicateNode("n", nil),
		}
		assert.False(t, p.Generic(e))
	})
}

func TestNamespacePredicate(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		event     any
		allowed   bool
	}{
		{
			"test create allowed",
			"kube-system",
			event.TypedCreateEvent[*coreV1.Event]{
				Object: &coreV1.Event{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "kube-system",
					},
				},
			},
			true,
		},
		{
			"test create denied",
			"kube-system",
			event.TypedCreateEvent[*coreV1.Event]{
				Object: &coreV1.Event{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "default",
					},
				},
			},
			false,
		},
		{
			"test delete allowed",
			"kube-system",
			event.TypedDeleteEvent[*coreV1.Event]{
				Object: &coreV1.Event{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "kube-system",
					},
				},
			},
			true,
		},
		{
			"test delete denied",
			"kube-system",
			event.TypedDeleteEvent[*coreV1.Event]{
				Object: &coreV1.Event{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "default",
					},
				},
			},
			false,
		},
		{
			"test update allowed",
			"kube-system",
			event.TypedUpdateEvent[*coreV1.Event]{
				ObjectNew: &coreV1.Event{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "kube-system",
					},
				},
			},
			true,
		},
		{
			"test update denied",
			"kube-system",
			event.TypedUpdateEvent[*coreV1.Event]{
				ObjectNew: &coreV1.Event{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "default",
					},
				},
			},
			false,
		},
		{
			"test generic allowed",
			"kube-system",
			event.TypedGenericEvent[*coreV1.Event]{
				Object: &coreV1.Event{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "kube-system",
					},
				},
			},
			true,
		},
		{
			"test generic denied",
			"kube-system",
			event.TypedGenericEvent[*coreV1.Event]{
				Object: &coreV1.Event{
					ObjectMeta: v1.ObjectMeta{
						Namespace: "default",
					},
				},
			},
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewNamespacePredicate[*coreV1.Event](tc.namespace)

			var res bool
			switch e := tc.event.(type) {
			case event.TypedCreateEvent[*coreV1.Event]:
				res = p.Create(e)
			case event.TypedDeleteEvent[*coreV1.Event]:
				res = p.Delete(e)
			case event.TypedUpdateEvent[*coreV1.Event]:
				res = p.Update(e)
			case event.TypedGenericEvent[*coreV1.Event]:
				res = p.Generic(e)
			}

			assert.Equal(t, tc.allowed, res)
		})
	}
}
