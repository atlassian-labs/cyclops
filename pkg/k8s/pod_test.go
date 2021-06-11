package k8s

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1beta1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testingCore "k8s.io/client-go/testing"

	"github.com/atlassian-labs/cyclops/pkg/test"
)

const testUnhealthyAfter = 5 * time.Minute

func podUnhealthyCondition(healthy bool, lastTransitionTime time.Time) corev1.PodCondition {
	statusCondition := corev1.ConditionFalse
	if healthy {
		statusCondition = corev1.ConditionTrue
	}
	return corev1.PodCondition{
		Type:               podConditionTypeForUnhealthy,
		Status:             statusCondition,
		LastTransitionTime: metav1.NewTime(lastTransitionTime),
	}
}

func timeNow() time.Time {
	return time.Unix(960579585, 0)
}

func TestEvictPod(t *testing.T) {
	pod := test.BuildTestPod(test.PodOpts{
		Name:      "test",
		Namespace: "kube-system",
		NodeName:  "test-node",
	})
	client, _ := test.BuildFakeClient(nil, []*corev1.Pod{pod})
	client.Fake.AddReactor("create", "pods", func(action testingCore.Action) (bool, runtime.Object, error) {
		assert.Equal(t, "eviction", action.GetSubresource())

		createAction := action.(testingCore.CreateAction)
		p := createAction.GetObject().(*policyv1.Eviction)

		assert.Equal(t, pod.Name, p.Name)
		return true, nil, nil
	})

	assert.Equal(t, nil, EvictPod(pod, "core/v1", client))
}

func TestEvictOrForciblyDeletePod(t *testing.T) {
	pod := test.BuildTestPod(test.PodOpts{
		Name:      "test",
		Namespace: "kube-system",
		NodeName:  "test-node",
	})
	pod.Status.Conditions = []corev1.PodCondition{podUnhealthyCondition(true, timeNow().Add(-10*time.Minute))}
	podEvictionAttempted := false
	podWasForcefullyDeleted := false

	client, _ := test.BuildFakeClient(nil, []*corev1.Pod{pod})
	client.Fake.AddReactor("create", "pods", func(action testingCore.Action) (bool, runtime.Object, error) {
		assert.Equal(t, "eviction", action.GetSubresource())

		createAction := action.(testingCore.CreateAction)
		p := createAction.GetObject().(*policyv1.Eviction)

		assert.Equal(t, pod.Name, p.Name)
		// Return a 429 error to test the code path of an unhealthy pod
		podEvictionAttempted = true
		return true, nil, apiErrors.NewTooManyRequests("pod cannot be removed and is unhealthy", 10)
	})
	client.Fake.AddReactor("delete", "pods", func(action testingCore.Action) (bool, runtime.Object, error) {
		podWasForcefullyDeleted = true
		return false, nil, nil
	})

	assert.Equal(t, nil, EvictOrForciblyDeletePod(pod, "core/v1", client, testUnhealthyAfter, timeNow()),
		"evict call expected to succeed")
	assert.Equal(t, true, podEvictionAttempted,
		"pod eviction should have been attempted")
	assert.Equal(t, false, podWasForcefullyDeleted,
		"pod deletion should not have been attempted since the pod is healthy")

	// Now the pod has been unhealthy for a while, the deletion attempt should happen
	pod.Status.Conditions = []corev1.PodCondition{podUnhealthyCondition(false, timeNow().Add(-10*time.Minute))}
	assert.Equal(t, nil, EvictOrForciblyDeletePod(pod, "core/v1", client, testUnhealthyAfter, timeNow()),
		"evict call expected to succeed")
	assert.Equal(t, true, podEvictionAttempted,
		"pod eviction should have been attempted")
	assert.Equal(t, true, podWasForcefullyDeleted,
		"pod deletion should have been attempted")
}

func TestEvictPods(t *testing.T) {
	var pods []*corev1.Pod
	pods = append(pods, test.BuildTestPod(test.PodOpts{
		Name:      "test-1",
		Namespace: "kube-system",
		NodeName:  "test-node",
	}))
	pods = append(pods, test.BuildTestPod(test.PodOpts{
		Name:      "test-2",
		Namespace: "kube-system",
		NodeName:  "test-node",
	}))
	pods = append(pods, test.BuildTestPod(test.PodOpts{
		Name:      "test-3",
		Namespace: "kube-system",
		NodeName:  "test-node",
	}))

	notFound := test.BuildTestPod(test.PodOpts{
		Name:      "test-missing",
		Namespace: "kube-system",
		NodeName:  "test-node",
	})

	// The unhealthy pod is quite complex to simulate as it has a timing element
	const unhealthyPodName = "test-unhealthy"
	unhealthyPod := test.BuildTestPod(test.PodOpts{
		Name:      unhealthyPodName,
		Namespace: "kube-system",
		NodeName:  "test-node",
	})
	unhealthyPod.Status.Conditions = []corev1.PodCondition{
		// Unhealthy, but for only 3 minutes so far
		podUnhealthyCondition(false, timeNow().Add(-3*time.Minute)),
	}
	unhealthyPodDeleted := false

	client, _ := test.BuildFakeClient(nil, pods)
	client.Fake.AddReactor("create", "pods", func(action testingCore.Action) (bool, runtime.Object, error) {
		assert.Equal(t, "eviction", action.GetSubresource())

		createAction := action.(testingCore.CreateAction)
		p := createAction.GetObject().(*policyv1.Eviction)

		for _, pod := range pods {
			if pod.Name == unhealthyPodName {
				return true, nil, apiErrors.NewTooManyRequests("", 10)
			}
			if pod.Name == p.Name {
				return true, pod, nil
			}
		}
		return true, nil, apiErrors.NewNotFound(schema.GroupResource{Group: "", Resource: "pods"}, p.Name)
	})
	client.Fake.AddReactor("delete", "pods", func(action testingCore.Action) (bool, runtime.Object, error) {
		unhealthyPodDeleted = true
		return false, nil, nil
	})

	assert.Equal(t, 0, len(EvictPods(pods, "core/v1", client, testUnhealthyAfter, timeNow())))
	pods = append(pods, notFound)
	assert.Equal(t, 0, len(EvictPods(pods, "core/v1", client, testUnhealthyAfter, timeNow())))
	pods = append(pods, unhealthyPod)
	assert.Equal(t, 0, len(EvictPods(pods, "core/v1", client, testUnhealthyAfter, timeNow())))
	assert.Equal(t, false, unhealthyPodDeleted, "unhealthy pod should not have been deleted yet")
	// Time passes such that the pod has been unhealthy for longer
	assert.Equal(t, 0, len(EvictPods(pods, "core/v1", client, testUnhealthyAfter, timeNow().Add(6*time.Minute))))
	assert.Equal(t, true, unhealthyPodDeleted, "unhealthy pod should be deleted")
}

func TestPodIsStatic(t *testing.T) {
	pod := test.BuildTestPod(test.PodOpts{
		Name: "test",
	})
	assert.Equal(t, false, PodIsStatic(pod))
	pod.Annotations = make(map[string]string)
	pod.Annotations["kubernetes.io/config.source"] = "abc"
	assert.Equal(t, false, PodIsStatic(pod))
	pod.Annotations["kubernetes.io/config.source"] = "file"
	assert.Equal(t, true, PodIsStatic(pod))
}

func TestPodIsDaemonSet(t *testing.T) {
	pod := test.BuildTestPod(test.PodOpts{
		Name: "test-xxxx",
	})
	assert.Equal(t, false, PodIsDaemonSet(pod))
	pod.OwnerReferences = append(pod.OwnerReferences, metav1.OwnerReference{
		APIVersion: "apps/v1",
		Kind:       "DaemonSet",
		Name:       "test",
	})
	assert.Equal(t, true, PodIsDaemonSet(pod))
}

func TestPodIsLongtermUnhealthy(t *testing.T) {
	pod := test.BuildTestPod(test.PodOpts{
		Name: "test-xxxx",
	})
	pod.Status.Conditions = []corev1.PodCondition{podUnhealthyCondition(true, time.Unix(0, 0))}
	assert.Equal(t, false, PodIsLongtermUnhealthy(pod.Status, testUnhealthyAfter, timeNow()),
		"pod condition is true, should not be unhealthy")

	pod.Status.Conditions[0] = podUnhealthyCondition(false, timeNow().Add(-30*time.Second))
	assert.Equal(t, false, PodIsLongtermUnhealthy(pod.Status, testUnhealthyAfter, timeNow()),
		"pod condition is false but last transition time is not long, so it should not be unhealthy")

	pod.Status.Conditions[0].LastTransitionTime = metav1.NewTime(timeNow().Add(-10 * time.Minute))
	assert.Equal(t, true, PodIsLongtermUnhealthy(pod.Status, testUnhealthyAfter, timeNow()),
		"pod condition is false and last transition time is a long time ago, so it should be unhealthy")
}
