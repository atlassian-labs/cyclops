package transitioner

import (
	"net/http"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/atlassian-labs/cyclops/pkg/mock"
)

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

// ************************************************************************** //

type Transitioner struct {
	*CycleNodeRequestTransitioner
	*mock.Client

	cloudProviderInstances []*mock.Node
	kubeNodes              []*mock.Node
}

func NewFakeTransitioner(cnr *v1.CycleNodeRequest, opts ...Option) *Transitioner {
	t := &Transitioner{
		// By default there are no nodes and each test will
		// override these as needed
		cloudProviderInstances: make([]*mock.Node, 0),
		kubeNodes:              make([]*mock.Node, 0),
	}

	for _, opt := range opts {
		opt(t)
	}

	t.Client = mock.NewClient(t.kubeNodes, t.cloudProviderInstances, cnr)

	rm := &controller.ResourceManager{
		Client:        t.K8sClient,
		RawClient:     t.RawClient,
		HttpClient:    http.DefaultClient,
		CloudProvider: t.CloudProvider,
	}

	t.CycleNodeRequestTransitioner = NewCycleNodeRequestTransitioner(
		cnr, rm, Options{},
	)

	return t
}
