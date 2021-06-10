package k8s

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	evictionKind        = "Eviction"
	evictionSubresource = "pods/eviction"
)

// DrainPods attempts to delete or evict pods so that the node can be terminated.
// Will prioritise using Evict if the API server supports it.
// Pods that have been unhealthy for longer than the given duration will be forcibly removed to prevent stalling.
func DrainPods(pods []*v1.Pod, client kubernetes.Interface, unhealthyAfter time.Duration) []error {
	// Determine whether we are able to delete or evict pods
	apiVersion, err := SupportEviction(client)
	if err != nil {
		return []error{err}
	}

	// If we are able to evict
	if len(apiVersion) == 0 {
		return []error{fmt.Errorf("apiVersion does not support pod eviction API")}
	}
	return EvictPods(pods, apiVersion, client, unhealthyAfter, time.Now())
}

// SupportEviction uses Discovery API to find out if the API server supports the eviction subresource
// If there is support, it will return its groupVersion; Otherwise, it will return ""
func SupportEviction(client kubernetes.Interface) (string, error) {
	discoveryClient := client.Discovery()
	groupList, err := discoveryClient.ServerGroups()
	if err != nil {
		return "", err
	}
	foundPolicyGroup := false
	var policyGroupVersion string
	for _, group := range groupList.Groups {
		if group.Name == "policy" {
			foundPolicyGroup = true
			policyGroupVersion = group.PreferredVersion.GroupVersion
			break
		}
	}
	if !foundPolicyGroup {
		return "", nil
	}
	resourceList, err := discoveryClient.ServerResourcesForGroupVersion("v1")
	if err != nil {
		return "", err
	}
	for _, resource := range resourceList.APIResources {
		if resource.Name == evictionSubresource && resource.Kind == evictionKind {
			return policyGroupVersion, nil
		}
	}
	return "", nil
}
