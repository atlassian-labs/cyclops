package transitioner

import (
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/controller"
)

type transitionFunc func() (reconcile.Result, error)

// cycleNodeLabel is placed on a node that is being worked on to allow
// more efficient filtering of node lists for large ASGs. The value is
// always the name of the CycleNodeRequest, to allow concurrency of
// different request types.
const cycleNodeLabel = "cyclops.atlassian.com/terminate"

var (
	transitionDuration = 10 * time.Second
	requeueDuration    = 30 * time.Second
)

// CycleNodeRequestTransitioner takes a cycleNodeRequest and attempts to transition it to the next phase
type CycleNodeRequestTransitioner struct {
	cycleNodeRequest *v1.CycleNodeRequest
	rm               *controller.ResourceManager
	options          Options
}

// Options stores configurable options for the CycleNodeRequestTransitioner
type Options struct {
	// DeleteCNR enables/disables deleting successful CycleNodeRequests after a certain amount of time
	DeleteCNR bool

	// DeleteCNRExpiry controls how long after the successful CycleNodeRequests was created to then try deleting it
	DeleteCNRExpiry time.Duration

	// DeleteCNRRequeue controls how often we should check if a CycleNodeRequest is ready to be deleted
	DeleteCNRRequeue time.Duration
}

// NewCycleNodeRequestTransitioner returns a new cycleNodeRequest transitioner
func NewCycleNodeRequestTransitioner(
	cycleNodeRequest *v1.CycleNodeRequest,
	rm *controller.ResourceManager,
	options Options,
) *CycleNodeRequestTransitioner {
	return &CycleNodeRequestTransitioner{
		cycleNodeRequest: cycleNodeRequest,
		rm:               rm,
		options:          options,
	}
}

// Run runs the CycleNodeRequestTransitioner and returns a reconcile result and an error
func (t *CycleNodeRequestTransitioner) Run() (reconcile.Result, error) {
	t.rm.Logger.Info("Transitioning cycleNodeRequest")

	// Locate the transition func for the phase
	transitionFuncs := t.transitionFuncs()
	tFunc, ok := transitionFuncs[t.cycleNodeRequest.Status.Phase]
	if !ok {
		err := fmt.Errorf("transition function not found for phase: %s", t.cycleNodeRequest.Status.Phase)
		t.rm.Logger.Error(err, "Unable to process cycleNodeRequest")
		return reconcile.Result{}, err
	}

	// Transition the cycleNodeRequest
	result, err := tFunc()
	if err != nil {
		t.rm.Logger.Error(err, "Error transitioning cycleNodeRequest")
	} else {
		t.rm.Logger.Info("Finished transitioning cycleNodeRequest", "requeue", result.Requeue, "requeue_after", result.RequeueAfter)
	}

	return result, err
}

func (t *CycleNodeRequestTransitioner) transitionFuncs() map[v1.CycleNodeRequestPhase]transitionFunc {
	return map[v1.CycleNodeRequestPhase]transitionFunc{
		v1.CycleNodeRequestUndefined:          t.transitionUndefined,
		v1.CycleNodeRequestPending:            t.transitionPending,
		v1.CycleNodeRequestInitialised:        t.transitionInitialised,
		v1.CycleNodeRequestScalingUp:          t.transitionScalingUp,
		v1.CycleNodeRequestCordoningNode:      t.transitionCordoning,
		v1.CycleNodeRequestWaitingTermination: t.transitionWaitingTermination,
		v1.CycleNodeRequestFailed:             t.transitionFailed,
		v1.CycleNodeRequestSuccessful:         t.transitionSuccessful,
		v1.CycleNodeRequestHealing:			   t.transitionHealing,
	}
}
