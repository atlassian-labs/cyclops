package mock

import (
	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/controller/cyclenoderequest/transitioner"
)

type Option func(m *MockClient)

func StartAtPhase(phase v1.CycleNodeRequestPhase) Option {
	return func(m *MockClient) {
		m.Cnr.Status.Phase = phase
	}
}

func WithCloudProviderInstances(nodes []*Node) Option {
	return func(m *MockClient) {
		m.cloudProviderInstances = append(m.cloudProviderInstances, nodes...)
	}
}

func WithKubeNodes(nodes []*Node) Option {
	return func(m *MockClient) {
		m.kubeNodes = append(m.kubeNodes, nodes...)
	}
}

func WithCycleNodeRequest(cycleNodeRequest *v1.CycleNodeRequest) Option {
	return func(m *MockClient) {
		m.Cnr = cycleNodeRequest
	}
}

func WithTransitionerOptions(options transitioner.Options) Option {
	return func(m *MockClient) {
		m.options = options
	}
}
