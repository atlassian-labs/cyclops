package cloudprovider

// CloudProvider provides an interface to interact with a cloud provider, e.g. AWS, GCP etc.
type CloudProvider interface {
	Name() string
	InstancesExist([]string) ([]string, error)
	GetNodeGroups([]string) (NodeGroups, error)
	TerminateInstance(string) error
}

// NodeGroups provides an interface to interact with a list of `node groups` in a cloud provider
// It handles different cloud provider's implementation of the node group.
// e.g. AWS's Autoscaling group, GCP's Instance group and so on.
type NodeGroups interface {
	Instances() map[string]Instance
	DetachInstance(string) (bool, error)
	AttachInstance(string, string) (bool, error)
	ReadyInstances() map[string]Instance
	NotReadyInstances() map[string]Instance
}

// Instance provides an interface to interact with an instance
type Instance interface {
	ID() string
	OutOfDate() bool
	MatchesProviderID(string) bool
	NodeGroupName() string
}
