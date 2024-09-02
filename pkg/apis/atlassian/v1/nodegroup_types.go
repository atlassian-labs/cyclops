package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeGroupSpec defines the desired state of NodeGroup
// +k8s:openapi-gen=true
type NodeGroupSpec struct {
	// NodeGroupName is the name of the node group in the cloud provider that corresponds to this NodeGroup resource.
	NodeGroupName string `json:"nodeGroupName"`

	// NodeGroupsList is a list of cloud provider node groups that corresponds to this NodeGroup resource.
	NodeGroupsList []string `json:"nodeGroupsList,omitempty"`

	// NodeSelector is the label selector used to select nodes that belong to this NodeGroup.
	NodeSelector metav1.LabelSelector `json:"nodeSelector"`

	// CycleSettings stores the settings to use for cycling the nodes.
	CycleSettings CycleSettings `json:"cycleSettings"`

	// MaxFailedCycleNodeRequests defines the maximum number of allowed failed CNRs for a nodegroup before the observer
	// stops generating them.
	MaxFailedCycleNodeRequests uint `json:"maxFailedCycleNodeRequests,omitempty"`

	// ValidationOptions stores the settings to use for validating state of nodegroups
	// in kube and the cloud provider for cycling the nodes.
	ValidationOptions ValidationOptions `json:"validationOptions,omitempty"`

	// Healthchecks stores the settings to configure instance custom health checks
	HealthChecks []HealthCheck `json:"healthChecks,omitempty"`

	// PreTerminationChecks stores the settings to configure instance pre-termination checks
	PreTerminationChecks []PreTerminationCheck `json:"preTerminationChecks,omitempty"`

	// SkipInitialHealthChecks is an optional flag to skip the initial set of node health checks before cycling begins
	// This does not affect the health checks performed as part of the pre-termination checks.
	SkipInitialHealthChecks bool `json:"skipInitialHealthChecks,omitempty"`

	// SkipPreTerminationChecks is an optional flag to skip pre-termination checks during cycling
	SkipPreTerminationChecks bool `json:"skipPreTerminationChecks,omitempty"`
}

// NodeGroupStatus defines the observed state of NodeGroup
// +k8s:openapi-gen=true
type NodeGroupStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeGroup is the Schema for the nodegroups API
// +k8s:openapi-gen=true
// +genclient:nonNamespaced
// +kubebuilder:resource:path=nodegroups,shortName=ng,scope=Cluster
// +kubebuilder:printcolumn:name="Node Group Name",type="string",JSONPath=".spec.nodeGroupName",description="The name of the node group in the cloud provider"
// +kubebuilder:printcolumn:name="Method",type="string",JSONPath=".spec.cycleSettings.method",description="The method to use when cycling nodes"
// +kubebuilder:printcolumn:name="Concurrency",type="integer",JSONPath=".spec.cycleSettings.concurrency",description="The number of nodes to cycle in parallel"
type NodeGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeGroupSpec   `json:"spec,omitempty"`
	Status NodeGroupStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeGroupList contains a list of NodeGroup
type NodeGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeGroup{}, &NodeGroupList{})
}
