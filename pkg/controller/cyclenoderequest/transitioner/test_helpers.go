package transitioner

import (
	"net/http"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/atlassian-labs/cyclops/pkg/mock"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Option func(t *Transitioner)

func WithCloudProviderInstances(nodes []*mock.Node) Option {
	return func(t *Transitioner) {
		t.CloudProviderInstances = append(t.CloudProviderInstances, nodes...)
	}
}

func WithKubeNodes(nodes []*mock.Node) Option {
	return func(t *Transitioner) {
		t.KubeNodes = append(t.KubeNodes, nodes...)
	}
}

func WithExtraKubeObject(extraKubeObject client.Object) Option {
	return func(t *Transitioner) {
		t.extraKubeObjects = append(t.extraKubeObjects, extraKubeObject)
	}
}

func WithTransitionerOptions(options Options) Option {
	return func(t *Transitioner) {
		t.transitionerOptions = options
	}
}

// ************************************************************************** //

type Transitioner struct {
	*CycleNodeRequestTransitioner
	*mock.Client

	CloudProviderInstances []*mock.Node
	KubeNodes              []*mock.Node

	extraKubeObjects []client.Object

	transitionerOptions Options
}

func NewFakeTransitioner(cnr *v1.CycleNodeRequest, opts ...Option) *Transitioner {
	t := &Transitioner{
		// By default there are no nodes and each test will
		// override these as needed
		CloudProviderInstances: make([]*mock.Node, 0),
		KubeNodes:              make([]*mock.Node, 0),
		extraKubeObjects:       []client.Object{cnr},
		transitionerOptions:    Options{},
	}

	for _, opt := range opts {
		opt(t)
	}

	t.Client = mock.NewClient(
		t.KubeNodes, t.CloudProviderInstances, t.extraKubeObjects...,
	)

	rm := &controller.ResourceManager{
		Client:        t.K8sClient,
		RawClient:     t.RawClient,
		HttpClient:    http.DefaultClient,
		CloudProvider: t.CloudProvider,
	}

	t.CycleNodeRequestTransitioner = NewCycleNodeRequestTransitioner(
		cnr, rm, t.transitionerOptions,
	)

	return t
}
