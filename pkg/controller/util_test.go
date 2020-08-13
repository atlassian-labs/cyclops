package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestNamespacePredicate(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		event     interface{}
		allowed   bool
	}{
		{
			"test create allowed",
			"kube-system",
			event.CreateEvent{
				Meta: &v1.ObjectMeta{
					Namespace: "kube-system",
				},
			},
			true,
		},
		{
			"test create denied",
			"kube-system",
			event.CreateEvent{
				Meta: &v1.ObjectMeta{
					Namespace: "default",
				},
			},
			false,
		},
		{
			"test delete allowed",
			"kube-system",
			event.DeleteEvent{
				Meta: &v1.ObjectMeta{
					Namespace: "kube-system",
				},
			},
			true,
		},
		{
			"test delete denied",
			"kube-system",
			event.DeleteEvent{
				Meta: &v1.ObjectMeta{
					Namespace: "default",
				},
			},
			false,
		},
		{
			"test update allowed",
			"kube-system",
			event.UpdateEvent{
				MetaNew: &v1.ObjectMeta{
					Namespace: "kube-system",
				},
			},
			true,
		},
		{
			"test update denied",
			"kube-system",
			event.UpdateEvent{
				MetaNew: &v1.ObjectMeta{
					Namespace: "default",
				},
			},
			false,
		},
		{
			"test generic allowed",
			"kube-system",
			event.GenericEvent{
				Meta: &v1.ObjectMeta{
					Namespace: "kube-system",
				},
			},
			true,
		},
		{
			"test generic denied",
			"kube-system",
			event.GenericEvent{
				Meta: &v1.ObjectMeta{
					Namespace: "default",
				},
			},
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewNamespacePredicate(tc.namespace)

			var res bool
			switch e := tc.event.(type) {
			case event.CreateEvent:
				res = p.Create(e)
			case event.DeleteEvent:
				res = p.Delete(e)
			case event.UpdateEvent:
				res = p.Update(e)
			case event.GenericEvent:
				res = p.Generic(e)
			}

			assert.Equal(t, tc.allowed, res)
		})
	}
}
