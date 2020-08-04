package k8s

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Patch describes a JSON Patch used to perform Patch operations on Kubernetes API objects via the API server.
type Patch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

// PatchPod patches a pod
func PatchPod(name, namespace string, patches []Patch, client kubernetes.Interface) error {
	data, err := json.Marshal(patches)
	if err != nil {
		return err
	}
	_, err = client.CoreV1().Pods(namespace).Patch(name, types.JSONPatchType, data)
	return err
}

// PatchNode patches a node
func PatchNode(name string, patches []Patch, client kubernetes.Interface) error {
	data, err := json.Marshal(patches)
	if err != nil {
		return err
	}
	_, err = client.CoreV1().Nodes().Patch(name, types.JSONPatchType, data)
	return err
}
