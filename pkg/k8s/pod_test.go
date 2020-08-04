package k8s

import (
	"testing"

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

	client, _ := test.BuildFakeClient(nil, pods)
	client.Fake.AddReactor("create", "pods", func(action testingCore.Action) (bool, runtime.Object, error) {
		assert.Equal(t, "eviction", action.GetSubresource())

		createAction := action.(testingCore.CreateAction)
		p := createAction.GetObject().(*policyv1.Eviction)

		for _, pod := range pods {
			if pod.Name == p.Name {
				return true, pod, nil
			}
		}
		return false, nil, apiErrors.NewNotFound(schema.GroupResource{Group: "", Resource: "pods"}, p.Name)
	})

	assert.Equal(t, 0, len(EvictPods(pods, "core/v1", client)))
	assert.Equal(t, 0, len(EvictPods(append(pods, notFound), "core/v1", client)))
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
