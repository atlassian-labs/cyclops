package manager_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/apis"
	cnrTransitioner "github.com/atlassian-labs/cyclops/pkg/controller/cyclenoderequest/transitioner"
	cnsTransitioner "github.com/atlassian-labs/cyclops/pkg/controller/cyclenodestatus/transitioner"
	nodecontroller "github.com/atlassian-labs/cyclops/pkg/controller/node"
	"github.com/atlassian-labs/cyclops/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"
)

// TestDependencies_ZeroValuesAreDocumented verifies that the Dependencies
// struct compiles and its fields are accessible. This is a lightweight guard
// against field renames breaking callers that build Dependencies directly.
func TestDependencies_ZeroValuesAreDocumented(t *testing.T) {
	deps := manager.Dependencies{
		Namespace:   "kube-system",
		CNROptions:  cnrTransitioner.Options{},
		CNSOptions:  cnsTransitioner.Options{},
		NodeOptions: nodecontroller.Options{},
	}
	assert.Equal(t, "kube-system", deps.Namespace)
	assert.Nil(t, deps.CloudProvider)
	assert.Nil(t, deps.Notifier)
}

// TestScheme_ContainsCyclopsTypes verifies that the cyclops scheme (registered
// by apis.AddToScheme, which Run calls internally) includes all three CRD
// types that cyclops controllers reconcile.
//
// This is the meaningful unit-testable part of Run: scheme registration is
// the first thing Run does, and if it fails Run returns an error immediately.
// The rest of Run (reconciler wiring, mgr.Start) requires a live apiserver
// and is covered by the integration tests.
func TestScheme_ContainsCyclopsTypes(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, apis.AddToScheme(scheme))

	for _, obj := range []runtime.Object{
		&atlassianv1.CycleNodeRequest{},
		&atlassianv1.CycleNodeRequestList{},
		&atlassianv1.CycleNodeStatus{},
		&atlassianv1.CycleNodeStatusList{},
		&atlassianv1.NodeGroup{},
		&atlassianv1.NodeGroupList{},
	} {
		gvks, _, err := scheme.ObjectKinds(obj)
		assert.NoError(t, err, "scheme should recognise %T", obj)
		assert.NotEmpty(t, gvks, "scheme should have at least one GVK for %T", obj)
	}
}
