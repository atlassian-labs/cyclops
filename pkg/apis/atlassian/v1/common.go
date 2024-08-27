package v1

// HasValidMethod returns true if the Method of the CycleSettings is a valid value.
func (in *CycleSettings) HasValidMethod() bool {
	switch in.Method {
	case CycleNodeRequestMethodDrain, CycleNodeRequestMethodWait:
		return true
	default:
		return false
	}
}

// buildNodeGroupNames builds a union of cloud provider node group names
// based on nodeGroupsList and nodeGroupName
func buildNodeGroupNames(nodeGroupsList []string, nodeGroupName string) []string {
	nodeGroupsMap := make(map[string]struct{})
	for _, ng := range nodeGroupsList {
		if len(ng) > 0 {
			nodeGroupsMap[ng] = struct{}{}
		}
	}

	if len(nodeGroupName) > 0 {
		nodeGroupsMap[nodeGroupName] = struct{}{}
	}

	var nodeGroups []string
	for k := range nodeGroupsMap {
		nodeGroups = append(nodeGroups, k)
	}

	return nodeGroups
}

// sameNodeGroups compares two lists of nodegroup names and check they are the
// same. Ordering does not affect equality.
func sameNodeGroups(groupA, groupB []string) bool {
	if len(groupA) != len(groupB) {
		return false
	}

	groupMap := make(map[string]struct{})
	for _, group := range groupA {
		groupMap[group] = struct{}{}
	}

	for _, group := range groupB {
		if _, ok := groupMap[group]; !ok {
			return false
		}
	}
	return true
}
