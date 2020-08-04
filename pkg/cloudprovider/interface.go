package cloudprovider

// CloudProvider provides an interface to interact with a cloud provider, e.g. AWS, GCP etc.
type CloudProvider interface {
	Name() string
	InstancesExist([]string) ([]string, error)
	GetNodeGroup(string) (NodeGroup, error)
	TerminateInstance(string) error
}

// NodeGroup provides an interface to interact with a "node group" in a cloud provider
// It handles different cloud provider's implementation of the node group.
// e.g. AWS's Autoscaling group, GCP's Instance group and so on.
type NodeGroup interface {
	ID() string
	Instances() []Instance
	DetachInstance(string) (bool, error)
	AttachInstance(string) (bool, error)
	ReadyInstances() []Instance
	NotReadyInstances() []Instance
}

// Instance provides an interface to interact with an instance
type Instance interface {
	ID() string
	OutOfDate() bool
	MatchesProviderID(string) bool
}
