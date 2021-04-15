package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CycleNodeStatusSpec defines the desired state of CycleNodeStatus
// +k8s:openapi-gen=true
type CycleNodeStatusSpec struct {
	// NodeName is the name of the node object in Kubernetes that will be drained and terminated.
	NodeName string `json:"nodeName"`

	// CycleSettings stores the settings to use for cycling the node.
	CycleSettings CycleSettings `json:"cycleSettings"`
}

// CycleNodeStatusStatus defines the observed state of a node being cycled by a CycleNodeRequest
// +k8s:openapi-gen=true
type CycleNodeStatusStatus struct {
	// Phase stores the current phase of the CycleNodeStatus
	Phase CycleNodeStatusPhase `json:"phase"`

	// A human readable message indicating details about why the CycleNodeStatus is in this condition
	Message string `json:"message"`

	// CurrentNode stores this node that is being "worked on"
	CurrentNode CycleNodeRequestNode `json:"currentNode"`

	// StartedTimestamp stores the timestamp that work on this node began
	StartedTimestamp *metav1.Time `json:"startedTimestamp,omitempty"`

	// TimeoutTimestamp stores the timestamp of when this CNS will timeout
	TimeoutTimestamp *metav1.Time `json:"timeoutTimestamp,omitempty"`
}

// CycleNodeStatusPhase is the phase that the cycleNodeStatus is in
type CycleNodeStatusPhase string

const (
	// CycleNodeStatusUndefined is for cycleNodeStatuses that aren't in any phase
	CycleNodeStatusUndefined CycleNodeStatusPhase = ""

	// CycleNodeStatusPending is for pending cycleNodeStatus
	CycleNodeStatusPending CycleNodeStatusPhase = "Pending"

	// CycleNodeStatusFailed is for failed cycleNodeStatuses
	CycleNodeStatusFailed CycleNodeStatusPhase = "Failed"

	// CycleNodeStatusRemovingLabelsFromPods is for cycleNodeStatuses that are removing labels from pods on the node
	CycleNodeStatusRemovingLabelsFromPods CycleNodeStatusPhase = "RemovingLabelsFromPods"

	// CycleNodeStatusWaitingPods is for cycleNodeStatuses that are waiting for pods to finish on the node
	CycleNodeStatusWaitingPods CycleNodeStatusPhase = "WaitingPods"

	// CycleNodeStatusDrainingPods is for cycleNodeStatuses that are draining pods from the node
	CycleNodeStatusDrainingPods CycleNodeStatusPhase = "DrainingPods"

	// CycleNodeStatusDeletingNode is for cycleNodeStatuses that are deleting the node out of the Kubernetes API
	CycleNodeStatusDeletingNode CycleNodeStatusPhase = "DeletingNode"

	// CycleNodeStatusTerminatingNode is for cyclenodeStatuses that are terminating the node from AWS
	CycleNodeStatusTerminatingNode CycleNodeStatusPhase = "TerminatingNode"

	// CycleNodeStatusSuccessful is for successful cycleNodeStatuses
	CycleNodeStatusSuccessful CycleNodeStatusPhase = "Successful"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CycleNodeStatus is the Schema for the cyclenodestatus API
// +k8s:openapi-gen=true
// +kubebuilder:resource:path=cyclenodestatuses,shortName=cns,scope=Namespaced
// +kubebuilder:printcolumn:name="Node",type="string",JSONPath=".status.currentNode.name",description="The name of the node"
// +kubebuilder:printcolumn:name="Provider ID",type="string",JSONPath=".status.currentNode.providerId",description="The provider ID of the node"
// +kubebuilder:printcolumn:name="Method",type="string",JSONPath=".spec.cycleSettings.method",description="The method being used for the cycle operation"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="The status of the request"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of the request"
type CycleNodeStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CycleNodeStatusSpec   `json:"spec,omitempty"`
	Status CycleNodeStatusStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CycleNodeStatusList contains a list of CycleNodeStatus
type CycleNodeStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CycleNodeStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CycleNodeStatus{}, &CycleNodeStatusList{})
}
