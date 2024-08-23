package transitioner

import (
	"context"
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/stretchr/testify/assert"

	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Test that if no delete options are given to the transitioner, then the CNR
// will not be deleted.
func TestSuccessfulNoDelete(t *testing.T) {
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-1",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestSuccessful,
			Message: "",
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr)

	var list v1.CycleNodeRequestList

	assert.NoError(t,
		fakeTransitioner.Client.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)

	// Execute the Successful phase
	_, err := fakeTransitioner.Run()
	assert.NoError(t, err)

	assert.NoError(t,
		fakeTransitioner.Client.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)
}

// Test that if the phase executes before the CNR deletion expiry time then the
// CNR won't be deleted.
func TestSuccessfulAfterDeleteTime(t *testing.T) {
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-1",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestSuccessful,
			Message: "",
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithTransitionerOptions(Options{
			DeleteCNR:       true,
			DeleteCNRExpiry: 0 * time.Second,
		}),
	)

	var list v1.CycleNodeRequestList

	assert.NoError(t,
		fakeTransitioner.Client.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)

	// Execute the Successful phase
	_, err := fakeTransitioner.Run()
	assert.NoError(t, err)

	assert.NoError(t,
		fakeTransitioner.Client.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 0)
}

// Test that if the phase executes after the CNR deletion expiry time then the
// CNR will be deleted.
func TestSuccessfulBeforeDeleteTime(t *testing.T) {
	cnr := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-1",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestSuccessful,
			Message: "",
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr,
		WithTransitionerOptions(Options{
			DeleteCNR:       true,
			DeleteCNRExpiry: 5 * time.Second,
		}),
	)

	var list v1.CycleNodeRequestList

	assert.NoError(t,
		fakeTransitioner.Client.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)

	// Execute the Successful phase
	_, err := fakeTransitioner.Run()
	assert.NoError(t, err)

	assert.NoError(t,
		fakeTransitioner.Client.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 1)
}

// Test that the Successful phase will delete sibling CNRs for the same
// nodegroup that are in the failed phase. No other CNRs should be deleted.
func TestSuccessfulDeleteFailedSiblingCNRs(t *testing.T) {
	// CNR used for the execution
	// Should not be deleted
	cnr1 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-1",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestSuccessful,
			Message: "",
		},
	}

	// Failed CNR for the same nodegroup
	// Should be deleted
	cnr2 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-2",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestFailed,
			Message: "",
		},
	}

	// Pending CNR for the same nodegroup
	// Should NOT be deleted
	cnr3 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-3",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestPending,
			Message: "",
		},
	}

	// Failed CNR for a different nodegroup
	// Should NOT be deleted
	cnr4 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-4",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-2",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestFailed,
			Message: "",
		},
	}

	// Failed CNR for the same nodegroup in a different namespace
	// Should NOT be deleted
	cnr5 := &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cnr-5",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1.CycleNodeRequestSpec{
			NodeGroupName: "ng-1",
		},
		Status: v1.CycleNodeRequestStatus{
			Phase:   v1.CycleNodeRequestFailed,
			Message: "",
		},
	}

	fakeTransitioner := NewFakeTransitioner(cnr1,
		WithExtraKubeObject(cnr2),
		WithExtraKubeObject(cnr3),
		WithExtraKubeObject(cnr4),
		WithExtraKubeObject(cnr5),
	)

	var list v1.CycleNodeRequestList

	assert.NoError(t,
		fakeTransitioner.Client.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 5)

	// Execute the Successful phase
	_, err := fakeTransitioner.Run()
	assert.NoError(t, err)

	assert.NoError(t,
		fakeTransitioner.Client.K8sClient.List(context.TODO(), &list, &client.ListOptions{}),
	)

	assert.Len(t, list.Items, 4)

	var cnr v1.CycleNodeRequest

	assert.NoError(t, fakeTransitioner.Client.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr1.Name,
		Namespace: cnr1.Namespace,
	}, &cnr))

	// CNR 2 is the only one that should be deleted
	assert.Error(t, fakeTransitioner.Client.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr2.Name,
		Namespace: cnr2.Namespace,
	}, &cnr))

	assert.NoError(t, fakeTransitioner.Client.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr3.Name,
		Namespace: cnr3.Namespace,
	}, &cnr))

	assert.NoError(t, fakeTransitioner.Client.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr4.Name,
		Namespace: cnr4.Namespace,
	}, &cnr))

	assert.NoError(t, fakeTransitioner.Client.K8sClient.Get(context.TODO(), types.NamespacedName{
		Name:      cnr5.Name,
		Namespace: cnr5.Namespace,
	}, &cnr))
}
