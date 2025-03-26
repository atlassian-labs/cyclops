package controller

import (
	coreV1 "k8s.io/api/core/v1"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

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
