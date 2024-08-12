package faketransitioner

import (
	"net/http"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/atlassian-labs/cyclops/pkg/controller/cyclenoderequest/transitioner"
	"github.com/atlassian-labs/cyclops/pkg/mock"
)

type Transitioner struct {
	*transitioner.CycleNodeRequestTransitioner
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

	t.CycleNodeRequestTransitioner = transitioner.NewCycleNodeRequestTransitioner(
		cnr, rm, transitioner.Options{},
	)

	return t
}
