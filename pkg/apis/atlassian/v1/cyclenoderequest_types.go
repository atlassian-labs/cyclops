package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CycleNodeRequestSpec defines the desired state of CycleNodeRequest
// +k8s:openapi-gen=true
type CycleNodeRequestSpec struct {
	// NodeGroupName is the name of the node group in the cloud provider that will be increased to bring
	// up replacement nodes.
	NodeGroupName string `json:"nodeGroupName"`

	// NodeGroupsList is a list of node groups in the cloud provider which includes target nodes
	// selected by node selector
	NodeGroupsList []string `json:"nodeGroupsList,omitempty"`

	// Selector is the label selector used to select the nodes that are to be terminated
	Selector metav1.LabelSelector `json:"selector"`

	// NodeNames is an optional list of the names of nodes to rotate. This is used to only
	// rotate specific nodes belonging to the NodeGroup, rather than every node in the group.
	// If no node names are provided in NodeNames then the entire node group will be rotated.
	NodeNames []string `json:"nodeNames,omitempty"`

	// CycleSettings stores the settings to use for cycling the nodes.
	CycleSettings CycleSettings `json:"cycleSettings"`

	// ValidationOptions stores the settings to use for validating state of nodegroups
	// in kube and the cloud provider for cycling the nodes.
	ValidationOptions ValidationOptions `json:"validationOptions,omitempty"`

	// HealthChecks stores the settings to configure instance custom health checks
	HealthChecks []HealthCheck `json:"healthChecks,omitempty"`

	// PreTerminationChecks stores the settings to configure instance pre-termination checks
	PreTerminationChecks []PreTerminationCheck `json:"preTerminationChecks,omitempty"`

	// SkipInitialHealthChecks is an optional flag to skip the initial set of node health checks before cycling begins
	// This does not affect the health checks performed as part of the pre-termination checks.
	SkipInitialHealthChecks bool `json:"skipInitialHealthChecks,omitempty"`

	// SkipPreTerminationChecks is an optional flag to skip pre-termination checks during cycling
	SkipPreTerminationChecks bool `json:"skipPreTerminationChecks,omitempty"`
}

// CycleNodeRequestStatus defines the observed state of CycleNodeRequest
// +k8s:openapi-gen=true
type CycleNodeRequestStatus struct {
	// Phase stores the current phase of the CycleNodeRequest
	Phase CycleNodeRequestPhase `json:"phase"`

	// A human readable message indicating details about why the CycleNodeRequest is in this condition.
	Message string `json:"message"`

	// CurrentNodes stores the current nodes that are being "worked on". Used to batch operations
	// against the node group in the cloud provider. Once a node is passed off to a CycleNodeStatus
	// CRD, it is no longer listed here.
	CurrentNodes []CycleNodeRequestNode `json:"currentNodes,omitempty"`

	// NodesToTerminate stores the old nodes that will be terminated.
	// The cycling of nodes is considered successful when all of these nodes no longer exist in the cluster.
	NodesToTerminate []CycleNodeRequestNode `json:"nodesToTerminate,omitempty"`

	// ScaleUpStarted stores the time when the scale up started
	// This is used to track the time limit of the scale up. If we breach the time limit
	// we fail the request.
	ScaleUpStarted *metav1.Time `json:"scaleUpStarted,omitempty"`

	// EquilibriumWaitStarted stores the time when we started waiting for equilibrium of Kube nodes and node group instances.
	// This is used to give some leeway if we start a request at the same time as a cluster scaling event.
	// If we breach the time limit we fail the request.
	EquilibriumWaitStarted *metav1.Time `json:"equilibriumWaitStarted,omitempty"`

	// ActiveChildren is the active number of CycleNodeStatuses that this CycleNodeRequest was aware of
	// when it last checked for progress in the cycle operation.
	ActiveChildren int64 `json:"activeChildren,omitempty"`

	// ThreadTimestamp is the timestamp of the thread in the messaging provider
	ThreadTimestamp string `json:"threadTimestamp,omitempty"`

	// SelectedNodes stores all selected nodes so that new nodes which are selected are only posted in a notification once
	SelectedNodes map[string]bool `json:"selectedNodes,omitempty"`

	// NumNodesCycled counts how many nodes have finished being cycled
	NumNodesCycled int `json:"numNodesCycled,omitempty"`

	// NodesAvailable stores the nodes still available to pick up for cycling from the list of nodes to terminate
	NodesAvailable []CycleNodeRequestNode `json:"nodesAvailable,omitempty"`

	// HealthChecks keeps track of instance health check information
	HealthChecks map[string]HealthCheckStatus `json:"healthChecks,omitempty"`

	// PreTerminationChecks keeps track of the instance pre termination check information
	PreTerminationChecks map[string]PreTerminationCheckStatusList `json:"preTerminationChecks,omitempty"`
}

// CycleNodeRequestNode stores a current node that is being worked on
type CycleNodeRequestNode struct {
	// Name of the node
	Name string `json:"name"`

	// Cloud Provider ID of the node
	ProviderID string `json:"providerId"`

	// NodeGroupName stores current cloud provider node group name
	// which this node belongs to
	NodeGroupName string `json:"nodeGroupName"`

	// Private ip of the instance
	PrivateIP string `json:"privateIp,omitempty"`
}

// ValidationOptions stores the settings to use for validating state of nodegroups
// in kube and the cloud provider for cycling the nodes.
type ValidationOptions struct {
	// SkipMissingNodeNames is a boolean which determines whether named nodes selected in a CNR must
	// exist and be valid nodes before cycling can begin. If set to true named nodes which don't exist
	// will be ignored rather than transitioning the CNR to the failed phase.
	SkipMissingNamedNodes bool `json:"skipMissingNamedNodes,omitempty"`
}

// HealthCheckStatus groups all health checks status information for a node
type HealthCheckStatus struct {
	// Ready keeps track of the first timestamp at which the node status was reported as "ready"
	NodeReady *metav1.Time `json:"ready,omitempty"`

	// Checks keeps track of the list of health checks performed on the node and which have already passed
	Checks []bool `json:"checks,omitempty"`

	// Skip denotes whether a node is part of a nodegroup before cycling has begun. If this is the case,
	// health checks on the instance are skipped, like this only new instances are checked.
	Skip bool `json:"skip,omitempty"`
}

// PreTerminationCheckStatusList groups all the PreTerminationCheckStatus for a node
type PreTerminationCheckStatusList struct {
	Checks []PreTerminationCheckStatus `json:"checks,omitempty"`
}

// PreTerminationCheckStatus groups all status information for the pre-termination trigger
// and ensuing heath checks
type PreTerminationCheckStatus struct {
	// Trigger marks the timestamp at which the trigger is sent.
	Trigger *metav1.Time `json:"trigger,omitempty"`

	// Check keeps track of health check result performed on the node
	Check bool `json:"check,omitempty"`
}

// CycleNodeRequestPhase is the phase that the cycleNodeRequest is in
type CycleNodeRequestPhase string

const (
	// CycleNodeRequestUndefined is for cycleNodeRequests that aren't in any phase
	CycleNodeRequestUndefined CycleNodeRequestPhase = ""

	// CycleNodeRequestPending is for pending cycleNodeRequests
	CycleNodeRequestPending CycleNodeRequestPhase = "Pending"

	// CycleNodeRequestFailed is for failed cycleNodeRequests
	CycleNodeRequestFailed CycleNodeRequestPhase = "Failed"

	// CycleNodeRequestInitialised is for initialised cycleNodeRequests
	CycleNodeRequestInitialised CycleNodeRequestPhase = "Initialised"

	// CycleNodeRequestScalingUp is for cycleNodeRequests that are scaling up
	CycleNodeRequestScalingUp CycleNodeRequestPhase = "ScalingUp"

	// CycleNodeRequestCordoningNode is for cycleNodeRequests that are cordoning the current node
	CycleNodeRequestCordoningNode CycleNodeRequestPhase = "CordoningNode"

	// CycleNodeRequestWaitingTermination is for cycleNodeRequests that are waiting for a current batch of nodes to terminate
	CycleNodeRequestWaitingTermination CycleNodeRequestPhase = "WaitingTermination"

	// CycleNodeRequestSuccessful is for successful cycleNodeRequests
	CycleNodeRequestSuccessful CycleNodeRequestPhase = "Successful"

	// CycleNodeRequestHealing is for the state before Failing where cyclops will try to put the cluster back in a consistent state
	CycleNodeRequestHealing CycleNodeRequestPhase = "Healing"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CycleNodeRequest is the Schema for the cyclenoderequests API
// +k8s:openapi-gen=true
// +kubebuilder:resource:path=cyclenoderequests,shortName=cnr,scope=Namespaced
// +kubebuilder:printcolumn:name="Node Group Name",type="string",JSONPath=".spec.nodeGroupName",description="The node group being cycled"
// +kubebuilder:printcolumn:name="Method",type="string",JSONPath=".spec.cycleSettings.method",description="The method being used for the cycle operation"
// +kubebuilder:printcolumn:name="Concurrency",type="integer",JSONPath=".spec.cycleSettings.concurrency",description="Max nodes the request is cycling at once"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="The status of the request"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of the request"
type CycleNodeRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	ClusterName string                 `json:"clusterName,omitempty"`
	Spec        CycleNodeRequestSpec   `json:"spec,omitempty"`
	Status      CycleNodeRequestStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CycleNodeRequestList contains a list of CycleNodeRequest
type CycleNodeRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CycleNodeRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CycleNodeRequest{}, &CycleNodeRequestList{})
}
