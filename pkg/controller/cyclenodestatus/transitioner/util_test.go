package transitioner

import (
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestTimedOut is a regression test for
// https://github.com/atlassian-labs/cyclops/issues/146. TimeoutTimestamp is
// optional: it is only set when the CycleNodeStatus is first created, so a
// CycleNodeStatus that reaches the DrainingPods phase without it must be
// treated as not timed out rather than panicking the controller.
func TestTimedOut(t *testing.T) {
	tests := []struct {
		name      string
		timeout   *metav1.Time
		wantTimed bool
	}{
		{
			name:      "no timeout timestamp",
			timeout:   nil,
			wantTimed: false,
		},
		{
			name:      "timeout in the past",
			timeout:   &metav1.Time{Time: time.Now().Add(-time.Minute)},
			wantTimed: true,
		},
		{
			name:      "timeout in the future",
			timeout:   &metav1.Time{Time: time.Now().Add(time.Minute)},
			wantTimed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cns := &v1.CycleNodeStatus{
				Status: v1.CycleNodeStatusStatus{
					Phase:            v1.CycleNodeStatusDrainingPods,
					TimeoutTimestamp: tc.timeout,
				},
			}

			assert.Equal(t, tc.wantTimed, NewCycleNodeStatusTransitioner(cns, nil, Options{}).timedOut())
		})
	}
}
