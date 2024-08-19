//go:build !ignore_autogenerated
// +build !ignore_autogenerated

// Code generated by operator-sdk. DO NOT EDIT.

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleNodeRequest) DeepCopyInto(out *CycleNodeRequest) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleNodeRequest.
func (in *CycleNodeRequest) DeepCopy() *CycleNodeRequest {
	if in == nil {
		return nil
	}
	out := new(CycleNodeRequest)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CycleNodeRequest) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleNodeRequestList) DeepCopyInto(out *CycleNodeRequestList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]CycleNodeRequest, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleNodeRequestList.
func (in *CycleNodeRequestList) DeepCopy() *CycleNodeRequestList {
	if in == nil {
		return nil
	}
	out := new(CycleNodeRequestList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CycleNodeRequestList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleNodeRequestNode) DeepCopyInto(out *CycleNodeRequestNode) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleNodeRequestNode.
func (in *CycleNodeRequestNode) DeepCopy() *CycleNodeRequestNode {
	if in == nil {
		return nil
	}
	out := new(CycleNodeRequestNode)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleNodeRequestSpec) DeepCopyInto(out *CycleNodeRequestSpec) {
	*out = *in
	if in.NodeGroupsList != nil {
		in, out := &in.NodeGroupsList, &out.NodeGroupsList
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.Selector.DeepCopyInto(&out.Selector)
	if in.NodeNames != nil {
		in, out := &in.NodeNames, &out.NodeNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.CycleSettings.DeepCopyInto(&out.CycleSettings)
	out.ValidationOptions = in.ValidationOptions
	if in.HealthChecks != nil {
		in, out := &in.HealthChecks, &out.HealthChecks
		*out = make([]HealthCheck, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.PreTerminationChecks != nil {
		in, out := &in.PreTerminationChecks, &out.PreTerminationChecks
		*out = make([]PreTerminationCheck, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleNodeRequestSpec.
func (in *CycleNodeRequestSpec) DeepCopy() *CycleNodeRequestSpec {
	if in == nil {
		return nil
	}
	out := new(CycleNodeRequestSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleNodeRequestStatus) DeepCopyInto(out *CycleNodeRequestStatus) {
	*out = *in
	if in.CurrentNodes != nil {
		in, out := &in.CurrentNodes, &out.CurrentNodes
		*out = make([]CycleNodeRequestNode, len(*in))
		copy(*out, *in)
	}
	if in.NodesToTerminate != nil {
		in, out := &in.NodesToTerminate, &out.NodesToTerminate
		*out = make([]CycleNodeRequestNode, len(*in))
		copy(*out, *in)
	}
	if in.ScaleUpStarted != nil {
		in, out := &in.ScaleUpStarted, &out.ScaleUpStarted
		*out = (*in).DeepCopy()
	}
	if in.EquilibriumWaitStarted != nil {
		in, out := &in.EquilibriumWaitStarted, &out.EquilibriumWaitStarted
		*out = (*in).DeepCopy()
	}
	if in.SelectedNodes != nil {
		in, out := &in.SelectedNodes, &out.SelectedNodes
		*out = make(map[string]bool, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.NodesAvailable != nil {
		in, out := &in.NodesAvailable, &out.NodesAvailable
		*out = make([]CycleNodeRequestNode, len(*in))
		copy(*out, *in)
	}
	if in.HealthChecks != nil {
		in, out := &in.HealthChecks, &out.HealthChecks
		*out = make(map[string]HealthCheckStatus, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
	if in.PreTerminationChecks != nil {
		in, out := &in.PreTerminationChecks, &out.PreTerminationChecks
		*out = make(map[string]PreTerminationCheckStatusList, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleNodeRequestStatus.
func (in *CycleNodeRequestStatus) DeepCopy() *CycleNodeRequestStatus {
	if in == nil {
		return nil
	}
	out := new(CycleNodeRequestStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleNodeStatus) DeepCopyInto(out *CycleNodeStatus) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleNodeStatus.
func (in *CycleNodeStatus) DeepCopy() *CycleNodeStatus {
	if in == nil {
		return nil
	}
	out := new(CycleNodeStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CycleNodeStatus) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleNodeStatusList) DeepCopyInto(out *CycleNodeStatusList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]CycleNodeStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleNodeStatusList.
func (in *CycleNodeStatusList) DeepCopy() *CycleNodeStatusList {
	if in == nil {
		return nil
	}
	out := new(CycleNodeStatusList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CycleNodeStatusList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleNodeStatusSpec) DeepCopyInto(out *CycleNodeStatusSpec) {
	*out = *in
	in.CycleSettings.DeepCopyInto(&out.CycleSettings)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleNodeStatusSpec.
func (in *CycleNodeStatusSpec) DeepCopy() *CycleNodeStatusSpec {
	if in == nil {
		return nil
	}
	out := new(CycleNodeStatusSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleNodeStatusStatus) DeepCopyInto(out *CycleNodeStatusStatus) {
	*out = *in
	out.CurrentNode = in.CurrentNode
	if in.StartedTimestamp != nil {
		in, out := &in.StartedTimestamp, &out.StartedTimestamp
		*out = (*in).DeepCopy()
	}
	if in.TimeoutTimestamp != nil {
		in, out := &in.TimeoutTimestamp, &out.TimeoutTimestamp
		*out = (*in).DeepCopy()
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleNodeStatusStatus.
func (in *CycleNodeStatusStatus) DeepCopy() *CycleNodeStatusStatus {
	if in == nil {
		return nil
	}
	out := new(CycleNodeStatusStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CycleSettings) DeepCopyInto(out *CycleSettings) {
	*out = *in
	if in.LabelsToRemove != nil {
		in, out := &in.LabelsToRemove, &out.LabelsToRemove
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.IgnorePodsLabels != nil {
		in, out := &in.IgnorePodsLabels, &out.IgnorePodsLabels
		*out = make(map[string][]string, len(*in))
		for key, val := range *in {
			var outVal []string
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = make([]string, len(*in))
				copy(*out, *in)
			}
			(*out)[key] = outVal
		}
	}
	if in.IgnoreNamespaces != nil {
		in, out := &in.IgnoreNamespaces, &out.IgnoreNamespaces
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.CyclingTimeout != nil {
		in, out := &in.CyclingTimeout, &out.CyclingTimeout
		*out = new(metav1.Duration)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CycleSettings.
func (in *CycleSettings) DeepCopy() *CycleSettings {
	if in == nil {
		return nil
	}
	out := new(CycleSettings)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HealthCheck) DeepCopyInto(out *HealthCheck) {
	*out = *in
	if in.WaitPeriod != nil {
		in, out := &in.WaitPeriod, &out.WaitPeriod
		*out = new(metav1.Duration)
		**out = **in
	}
	if in.ValidStatusCodes != nil {
		in, out := &in.ValidStatusCodes, &out.ValidStatusCodes
		*out = make([]uint, len(*in))
		copy(*out, *in)
	}
	out.TLSConfig = in.TLSConfig
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HealthCheck.
func (in *HealthCheck) DeepCopy() *HealthCheck {
	if in == nil {
		return nil
	}
	out := new(HealthCheck)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HealthCheckStatus) DeepCopyInto(out *HealthCheckStatus) {
	*out = *in
	if in.NodeReady != nil {
		in, out := &in.NodeReady, &out.NodeReady
		*out = (*in).DeepCopy()
	}
	if in.Checks != nil {
		in, out := &in.Checks, &out.Checks
		*out = make([]bool, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HealthCheckStatus.
func (in *HealthCheckStatus) DeepCopy() *HealthCheckStatus {
	if in == nil {
		return nil
	}
	out := new(HealthCheckStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeGroup) DeepCopyInto(out *NodeGroup) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeGroup.
func (in *NodeGroup) DeepCopy() *NodeGroup {
	if in == nil {
		return nil
	}
	out := new(NodeGroup)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NodeGroup) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeGroupList) DeepCopyInto(out *NodeGroupList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]NodeGroup, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeGroupList.
func (in *NodeGroupList) DeepCopy() *NodeGroupList {
	if in == nil {
		return nil
	}
	out := new(NodeGroupList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NodeGroupList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeGroupSpec) DeepCopyInto(out *NodeGroupSpec) {
	*out = *in
	if in.NodeGroupsList != nil {
		in, out := &in.NodeGroupsList, &out.NodeGroupsList
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.NodeSelector.DeepCopyInto(&out.NodeSelector)
	in.CycleSettings.DeepCopyInto(&out.CycleSettings)
	out.ValidationOptions = in.ValidationOptions
	if in.HealthChecks != nil {
		in, out := &in.HealthChecks, &out.HealthChecks
		*out = make([]HealthCheck, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.PreTerminationChecks != nil {
		in, out := &in.PreTerminationChecks, &out.PreTerminationChecks
		*out = make([]PreTerminationCheck, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeGroupSpec.
func (in *NodeGroupSpec) DeepCopy() *NodeGroupSpec {
	if in == nil {
		return nil
	}
	out := new(NodeGroupSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeGroupStatus) DeepCopyInto(out *NodeGroupStatus) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeGroupStatus.
func (in *NodeGroupStatus) DeepCopy() *NodeGroupStatus {
	if in == nil {
		return nil
	}
	out := new(NodeGroupStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreTerminationCheck) DeepCopyInto(out *PreTerminationCheck) {
	*out = *in
	if in.ValidStatusCodes != nil {
		in, out := &in.ValidStatusCodes, &out.ValidStatusCodes
		*out = make([]uint, len(*in))
		copy(*out, *in)
	}
	in.HealthCheck.DeepCopyInto(&out.HealthCheck)
	out.TLSConfig = in.TLSConfig
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreTerminationCheck.
func (in *PreTerminationCheck) DeepCopy() *PreTerminationCheck {
	if in == nil {
		return nil
	}
	out := new(PreTerminationCheck)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreTerminationCheckStatus) DeepCopyInto(out *PreTerminationCheckStatus) {
	*out = *in
	if in.Trigger != nil {
		in, out := &in.Trigger, &out.Trigger
		*out = (*in).DeepCopy()
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreTerminationCheckStatus.
func (in *PreTerminationCheckStatus) DeepCopy() *PreTerminationCheckStatus {
	if in == nil {
		return nil
	}
	out := new(PreTerminationCheckStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PreTerminationCheckStatusList) DeepCopyInto(out *PreTerminationCheckStatusList) {
	*out = *in
	if in.Checks != nil {
		in, out := &in.Checks, &out.Checks
		*out = make([]PreTerminationCheckStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PreTerminationCheckStatusList.
func (in *PreTerminationCheckStatusList) DeepCopy() *PreTerminationCheckStatusList {
	if in == nil {
		return nil
	}
	out := new(PreTerminationCheckStatusList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TLSConfig) DeepCopyInto(out *TLSConfig) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TLSConfig.
func (in *TLSConfig) DeepCopy() *TLSConfig {
	if in == nil {
		return nil
	}
	out := new(TLSConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ValidationOptions) DeepCopyInto(out *ValidationOptions) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ValidationOptions.
func (in *ValidationOptions) DeepCopy() *ValidationOptions {
	if in == nil {
		return nil
	}
	out := new(ValidationOptions)
	in.DeepCopyInto(out)
	return out
}
