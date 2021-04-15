package transitioner

import (
	"fmt"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type transitionFunc func() (reconcile.Result, error)

var (
	transitionDuration = 10 * time.Second
)

// CycleNodeStatusTransitioner takes a cycleNodeStatus and attempts to transition it to the next phase
type CycleNodeStatusTransitioner struct {
	cycleNodeStatus *v1.CycleNodeStatus
	rm              *controller.ResourceManager
	options         Options
}

// NewCycleNodeStatusTransitioner returns a new cycleNodeStatus transitioner
func NewCycleNodeStatusTransitioner(
	cycleNodeStatus *v1.CycleNodeStatus,
	rm *controller.ResourceManager,
	options Options,
) *CycleNodeStatusTransitioner {
	return &CycleNodeStatusTransitioner{
		cycleNodeStatus: cycleNodeStatus,
		rm:              rm,
		options:         options,
	}
}

// Options stores configurable options for the NewCycleNodeStatusTransitioner
type Options struct {
	// DefaultCNScyclingExpiry controls how long until the CycleNodeStatus will timeout
	DefaultCNScyclingExpiry time.Duration
}

// Run runs the CycleNodeStatusTransitioner and returns a reconcile result and an error
func (t *CycleNodeStatusTransitioner) Run() (reconcile.Result, error) {
	t.rm.Logger.Info("Transitioning cycleNodeStatus")

	// Locate the transition func for the phase
	transitionFuncs := t.transitionFuncs()
	tFunc, ok := transitionFuncs[t.cycleNodeStatus.Status.Phase]
	if !ok {
		err := fmt.Errorf("transition function not found for phase: %s", t.cycleNodeStatus.Status.Phase)
		t.rm.Logger.Error(err, "Unable to process cycleNodeStatus")
		return reconcile.Result{}, err
	}

	// Transition the cycleNodeStatus
	result, err := tFunc()
	if err != nil {
		t.rm.Logger.Error(err, "Error transitioning cycleNodeStatus")
	} else {
		t.rm.Logger.Info("Finished transitioning cycleNodeStatus", "requeue", result.Requeue, "requeue_after", result.RequeueAfter)
	}

	return result, err
}

func (t *CycleNodeStatusTransitioner) transitionFuncs() map[v1.CycleNodeStatusPhase]transitionFunc {
	return map[v1.CycleNodeStatusPhase]transitionFunc{
		v1.CycleNodeStatusUndefined:              t.transitionUndefined,
		v1.CycleNodeStatusPending:                t.transitionPending,
		v1.CycleNodeStatusWaitingPods:            t.transitionWaitingPods,
		v1.CycleNodeStatusRemovingLabelsFromPods: t.transitionRemovingLabelsFromPods,
		v1.CycleNodeStatusDrainingPods:           t.transitionDraining,
		v1.CycleNodeStatusDeletingNode:           t.transitionDeleting,
		v1.CycleNodeStatusTerminatingNode:        t.transitionTerminating,
		v1.CycleNodeStatusFailed:                 t.transitionFailed,
		v1.CycleNodeStatusSuccessful:             t.transitionSuccessful,
	}
}
