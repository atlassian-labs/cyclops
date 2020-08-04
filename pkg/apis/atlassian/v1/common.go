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
