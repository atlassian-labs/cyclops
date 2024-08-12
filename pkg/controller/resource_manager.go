package controller

import (
	"context"
	"net/http"

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
	HttpClient    *http.Client
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
	httpClient *http.Client,
	recorder record.EventRecorder,
	logger logr.Logger,
	notifier notifications.Notifier,
	cloudProvider cloudprovider.CloudProvider,
) *ResourceManager {
	return &ResourceManager{
		Client:        client,
		RawClient:     rawClient,
		HttpClient:    httpClient,
		Recorder:      recorder,
		Logger:        logger,
		Notifier:      notifier,
		CloudProvider: cloudProvider,
	}
}

// UpdateObject updates the given object in the API
func (rm *ResourceManager) UpdateObject(obj client.Object) error {
	err := rm.Client.Update(context.TODO(), obj)
	if err != nil {
		rm.Logger.Error(err, "unable to update API object",
			"objectType", obj.GetObjectKind().GroupVersionKind().String())
	}
	return err
}

// LogEvent creates an event on the current object
func (rm *ResourceManager) LogEvent(obj runtime.Object, reason, messageFmt string, args ...interface{}) {
	if rm.Recorder != nil {
		rm.Recorder.Eventf(obj, coreV1.EventTypeNormal, reason, messageFmt, args...)
	}
}

// LogWarningEvent creates a warning event on the current object
func (rm *ResourceManager) LogWarningEvent(obj runtime.Object, reason, messageFmt string, args ...interface{}) {
	if rm.Recorder != nil {
		rm.Recorder.Eventf(obj, coreV1.EventTypeWarning, reason, messageFmt, args...)
	}
}
