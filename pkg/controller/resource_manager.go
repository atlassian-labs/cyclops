package controller

import (
	"context"

	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/atlassian-labs/cyclops/pkg/notifications"
	"github.com/go-logr/logr"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResourceManager is a struct which provides some consistency across controllers.
// It contains all of the common fields that controllers will need to use.
// It provides helpers that use the controller-runtime client.Client interface to interact with Kubernetes.
type ResourceManager struct {
	Client        client.Client
	RawClient     kubernetes.Interface
	Recorder      record.EventRecorder
	Logger        logr.Logger
	Notifier      notifications.Notifier
	CloudProvider cloudprovider.CloudProvider
	Namespace     string
}

// NewResourceManager creates a new ResourceManager
func NewResourceManager(
	client client.Client,
	rawClient kubernetes.Interface,
	recorder record.EventRecorder,
	logger logr.Logger,
	notifier notifications.Notifier,
	cloudProvider cloudprovider.CloudProvider,
) *ResourceManager {
	return &ResourceManager{
		Client:        client,
		RawClient:     rawClient,
		Recorder:      recorder,
		Logger:        logger,
		Notifier:      notifier,
		CloudProvider: cloudProvider,
	}
}

// UpdateObject updates the given object in the API
func (t *ResourceManager) UpdateObject(obj client.Object) error {
	err := t.Client.Update(context.TODO(), obj)
	if err != nil {
		t.Logger.Error(err, "unable to update API object",
			"objectType", obj.GetObjectKind().GroupVersionKind().String())
	}
	return err
}

// LogEvent creates an event on the current object
func (t *ResourceManager) LogEvent(obj runtime.Object, reason, messageFmt string, args ...interface{}) {
	t.Recorder.Eventf(obj, coreV1.EventTypeNormal, reason, messageFmt, args...)
}

// LogWarningEvent creates a warning event on the current object
func (t *ResourceManager) LogWarningEvent(obj runtime.Object, reason, messageFmt string, args ...interface{}) {
	t.Recorder.Eventf(obj, coreV1.EventTypeWarning, reason, messageFmt, args...)
}
