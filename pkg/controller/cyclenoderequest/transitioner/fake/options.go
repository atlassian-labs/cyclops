package faketransitioner

import "github.com/atlassian-labs/cyclops/pkg/mock"

type Option func(t *Transitioner)

func WithCloudProviderInstances(nodes []*mock.Node) Option {
	return func(t *Transitioner) {
		t.cloudProviderInstances = append(t.cloudProviderInstances, nodes...)
	}
}

func WithKubeNodes(nodes []*mock.Node) Option {
	return func(t *Transitioner) {
		t.kubeNodes = append(t.kubeNodes, nodes...)
	}
}
