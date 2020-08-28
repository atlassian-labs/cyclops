package v1

// GetNodeGroupNames gets a list of cloud provider node group names
// based on NodeGroupSpec `NodeGroupName` and `NodeGroupsList`
func (in *NodeGroup) GetNodeGroupNames() []string {
	return buildNodeGroupNames(in.Spec.NodeGroupsList, in.Spec.NodeGroupName)
}
