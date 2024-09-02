package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetUndisruptablePods(t *testing.T) {
	pod1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
			Annotations: map[string]string{
				"cyclops.atlassian.com/do-not-disrupt": "true",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	pod2 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-2",
			Annotations: map[string]string{
				"cyclops.atlassian.com/do-not-disrupt": "true",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}

	pod3 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-3",
			Annotations: map[string]string{
				"cyclops.atlassian.com/do-not-disrupt": "false",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	pod4 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-4",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	tests := []struct {
		name string
		pods []corev1.Pod
		want []corev1.Pod
	}{
		{
			"test with no pods with annotation",
			[]corev1.Pod{pod4},
			[]corev1.Pod{},
		},
		{
			"test with 1 pod with annotation",
			[]corev1.Pod{pod1},
			[]corev1.Pod{pod1},
		},
		{
			"test succeeded pod with annotation",
			[]corev1.Pod{pod2},
			[]corev1.Pod{},
		},
		{
			"test with 1 pod without correct annotation value",
			[]corev1.Pod{pod3},
			[]corev1.Pod{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.ElementsMatch(t, tc.want, getUndisruptablePods(tc.pods))
		})
	}
}
