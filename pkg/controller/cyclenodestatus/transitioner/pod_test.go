package transitioner

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var runningPodList = []corev1.Pod{
	pod("dpod1", "default", "filter1=yes", "filter2=no"),
	pod("dpod2", "default", "filter1=no", "filter2=yes"),
	pod("dpod3", "default"),
	pod("kspod1", "kube-system", "filter1=yes", "filter2=no"),
	pod("kspod2", "kube-system", "filter1=no", "filter2=yes"),
	pod("kspod3", "kube-system"),
}

func pod(name string, namespace string, labels ...string) corev1.Pod {
	// Label strings are of the format 'key=value'
	podLabels := make(map[string]string)
	for _, s := range labels {
		keyValue := strings.Split(s, "=")
		podLabels[keyValue[0]] = keyValue[1]
	}

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    podLabels,
		},
	}
	return pod
}

func TestGetRunningPods(t *testing.T) {
	tests := []struct {
		name             string
		pods             []corev1.Pod
		ignoreNamespaces []string
		ignorePodsLabels map[string][]string
		want             []corev1.Pod
	}{
		{
			"no filtering applied",
			runningPodList,
			[]string{},
			map[string][]string{},
			runningPodList,
		},
		{
			"default namespace filtered",
			runningPodList,
			[]string{"default"},
			map[string][]string{},
			[]corev1.Pod{
				pod("kspod1", "kube-system", "filter1=yes", "filter2=no"),
				pod("kspod2", "kube-system", "filter1=no", "filter2=yes"),
				pod("kspod3", "kube-system"),
			},
		},
		{
			"kube-system namespace filtered",
			runningPodList,
			[]string{"kube-system"},
			map[string][]string{},
			[]corev1.Pod{
				pod("dpod1", "default", "filter1=yes", "filter2=no"),
				pod("dpod2", "default", "filter1=no", "filter2=yes"),
				pod("dpod3", "default"),
			},
		},
		{
			"filter1=yes label filtered",
			runningPodList,
			[]string{},
			map[string][]string{
				"filter1": {"yes"},
			},
			[]corev1.Pod{
				pod("dpod2", "default", "filter1=no", "filter2=yes"),
				pod("dpod3", "default"),
				pod("kspod2", "kube-system", "filter1=no", "filter2=yes"),
				pod("kspod3", "kube-system"),
			},
		},
		{
			"filter1=yes, filter2=yes labels filtered",
			runningPodList,
			[]string{},
			map[string][]string{
				"filter1": {"yes"},
				"filter2": {"yes"},
			},
			[]corev1.Pod{
				pod("dpod3", "default"),
				pod("kspod3", "kube-system"),
			},
		},
		{
			"filter1 in (yes, no) label filtered",
			runningPodList,
			[]string{},
			map[string][]string{
				"filter1": {"yes", "no"},
			},
			[]corev1.Pod{
				pod("dpod3", "default"),
				pod("kspod3", "kube-system"),
			},
		},
		{
			"filter1 in (yes, no) label filtered, default namespace filtered",
			runningPodList,
			[]string{"default"},
			map[string][]string{
				"filter1": {"yes", "no"},
			},
			[]corev1.Pod{
				pod("kspod3", "kube-system"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := getRunningPods(tc.pods, tc.ignoreNamespaces, tc.ignorePodsLabels)
			if err != nil {
				t.Errorf("getRunningPods(runningPodList, %+v, %+v) error: %v", tc.ignoreNamespaces, tc.ignorePodsLabels, err)
			}

			for _, wantPod := range tc.want {
				gotWanted := false
				for _, gotPod := range got {
					if wantPod.Name == gotPod.Name && wantPod.Namespace == gotPod.Namespace {
						gotWanted = true
						break
					}
				}
				if !gotWanted {
					t.Errorf("getRunningPods(runningPodList, %+v, %+v): could not find wanted pod: {Name: %v, Namespace: %v}", tc.ignoreNamespaces, tc.ignorePodsLabels, wantPod.Name, wantPod.Namespace)
				}
			}
		})
	}
}
