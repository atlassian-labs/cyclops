package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CycleNodeRequestMethod is the method to use when cycling nodes.
type CycleNodeRequestMethod string

const (
	// CycleNodeRequestMethodDrain actively drains pods off the node before terminating it.
	// This is the default method.
	CycleNodeRequestMethodDrain = "Drain"

	// CycleNodeRequestMethodWait waits for pods to leave the node before terminating it.
	// It will ignore DaemonSets and select pods. These can be configured in the CRD spec.
	CycleNodeRequestMethodWait = "Wait"
)

// CycleSettings are configuration options to control how nodes are cycled
// +k8s:openapi-gen=true
type CycleSettings struct {
	// Method describes the type of cycle operation to use.
	// +kubebuilder:validation:Enum=Drain;Wait
	Method CycleNodeRequestMethod `json:"method"`

	// Concurrency is the number of nodes that one CycleNodeRequest will work on in parallel.
	// Defaults to the size of the node group.
	Concurrency int64 `json:"concurrency,omitempty"`

	// LabelsToRemove is an array of labels to remove off of the pods running on the node
	// This can be used to remove a pod from a service/endpoint before evicting/deleting
	// it to prevent traffic being sent to it.
	LabelsToRemove []string `json:"labelsToRemove,omitempty"`

	// IgnorePodLabels is a map of values for labels that describes which pods should be ignored when
	// deciding whether a node has no more pods running. This map defines a union: any pod that
	// matches any of the values for a given label name will be ignored.
	IgnorePodsLabels map[string][]string `json:"ignorePodsLabels,omitempty"`

	// IgnoreNamespaces is a list of namespace names in which running pods should be ignored
	// when deciding whether a node has no more pods running.
	IgnoreNamespaces []string `json:"ignoreNamespaces,omitempty"`

	// CyclingTimeout is a string in time duration format that defines how long a until an
	// in-progress CNS request timeout from the time it's worked on by the controller.
	// If no cyclingTimeout is provided, CNS will use the default controller CNS cyclingTimeout.
	CyclingTimeout *metav1.Duration `json:"cyclingTimeout,omitempty"`
}

// HealthCheck defines the health check configuration for the NodeGroup
// +k8s:openapi-gen=true
type HealthCheck struct {
	// Endpoint url of the health check. Optional: {{ .NodeIP }} gets replaced by the private IP of the node being scaled up.
	Endpoint string `json:"endpoint"`

	// WaitPeriod is the time allowed for the health check to pass before considering the
	// service unhealthy and failing the CycleNodeRequest.
	WaitPeriod *metav1.Duration `json:"waitPeriod"`

	// ValidStatusCodes keeps track of the list of possible status codes returned by
	// the endpoint denoting the service as healthy. Defaults to [200].
	ValidStatusCodes []uint `json:"validStatusCodes,omitempty"`

	// RegexMatch specifies a regex string the body of the http result to should. By default no matching is done.
	RegexMatch string `json:"regexMatch,omitempty"`

	// TLS configuration for the http client to make requests. Can either make standard https requests
	// or optionally forward certs signed by the root CA for mTLS.
	TLSConfig `json:"tls,omitempty"`
}

// PreTerminationCheck defines the configuration for the check done before terminating an instance. The trigger can be
// considered a http sigterm and the subsequent check to know when the process has completed it's triggered action.
// +k8s:openapi-gen=true
type PreTerminationCheck struct {
	// Endpoint url of the health check. Optional: {{ .NodeIP }} gets replaced by the private IP of the node being scaled up.
	Endpoint string `json:"triggerEndpoint"`

	// ValidStatusCodes keeps track of the list of possible status codes returned by
	// the endpoint denoting the service as healthy. Defaults to [200].
	ValidStatusCodes []uint `json:"validStatusCodes,omitempty"`

	// HealthCheck denotes the configuration for performing health checks after the trigger has been sent. This works the
	// exact same way as health check on new nodes.
	HealthCheck `json:"healthCheck"`

	// TLS configuration for the http client to make requests. Can either make standard https requests
	// or optionally forward certs signed by the root CA for mTLS.
	TLSConfig `json:"tls,omitempty"`
}

// TLSConfig defined the tls configuration for the http client to make a request.
// +k8s:openapi-gen=true
type TLSConfig struct {
	// RootCA is the root CA shared between Cyclops and the upstream host.
	RootCA string `json:"rootCA,omitempty"`

	// Certificate is the crt given to Cyclops for mTLS. It is sent as part
	// of the request to the upstream host.
	Certificate string `json:"crt,omitempty"`

	// Key is the private key which forms a pair with the certificate. It is
	// sent as part of the request to the upstream host for mTLS.
	Key string `json:"key,omitempty"`
}

// ValidationOptions stores the settings to use for validating state of nodegroups
// in kube and the cloud provider for cycling the nodes.
type ValidationOptions struct {
	// SkipMissingNodeNames is a boolean which determines whether named nodes selected in a CNR must
	// exist and be valid nodes before cycling can begin. If set to true named nodes which don't exist
	// will be ignored rather than transitioning the CNR to the failed phase.
	SkipMissingNamedNodes bool `json:"skipMissingNamedNodes,omitempty"`
}
