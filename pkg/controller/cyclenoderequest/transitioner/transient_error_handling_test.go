package transitioner

import (
	"errors"
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/aws/aws-sdk-go/aws/awserr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// mockCloudProviderWithTransientErrors simulates AWS transient failures
type mockCloudProviderWithTransientErrors struct {
	callCount          int
	failuresBeforeSuccess int
	errorToReturn      error
}

func (m *mockCloudProviderWithTransientErrors) GetNodeGroups(names []string) (cloudprovider.NodeGroups, error) {
	m.callCount++
	if m.callCount <= m.failuresBeforeSuccess {
		return nil, m.errorToReturn
	}
	// Success after N failures
	return &mockNodeGroups{}, nil
}

func (m *mockCloudProviderWithTransientErrors) InstancesExist(providerIDs []string) (map[string]interface{}, error) {
	return make(map[string]interface{}), nil
}

func (m *mockCloudProviderWithTransientErrors) TerminateInstance(providerID string) error {
	return nil
}

func (m *mockCloudProviderWithTransientErrors) DetachInstance(nodeGroupName, providerID string) error {
	return nil
}

func (m *mockCloudProviderWithTransientErrors) AttachInstance(nodeGroupName, providerID string) error {
	return nil
}

func (m *mockCloudProviderWithTransientErrors) AddInstanceToNodeGroup(nodeGroupName string, nodeGroup cloudprovider.NodeGroupOptions) error {
	return nil
}

func (m *mockCloudProviderWithTransientErrors) Name() string {
	return "mock-aws"
}

// mockNodeGroups is a simple mock implementation
type mockNodeGroups struct{}

func (m *mockNodeGroups) Instances() map[string]cloudprovider.Instance {
	return map[string]cloudprovider.Instance{
		"aws:///us-west-2a/i-1234567890abcdef0": &mockInstance{
			providerID: "aws:///us-west-2a/i-1234567890abcdef0",
		},
	}
}

func (m *mockNodeGroups) InstanceNames() []string {
	return []string{"test-node-1"}
}

func (m *mockNodeGroups) NewestInstance() (cloudprovider.Instance, error) {
	return &mockInstance{providerID: "aws:///us-west-2a/i-1234567890abcdef0"}, nil
}

type mockInstance struct {
	providerID string
}

func (m *mockInstance) ProviderID() string {
	return m.providerID
}

func (m *mockInstance) NodeGroupName() string {
	return "test-nodegroup"
}

func (m *mockInstance) OutOfDate() bool {
	return false
}

// TestTransitionPending_TransientErrorRequeue tests that transient errors cause requeue instead of Healing
// This reproduces the original issue where i/o timeout caused immediate Healing transition
func TestTransitionPending_TransientErrorRequeue(t *testing.T) {
	tests := []struct {
		name                  string
		error                 error
		expectRequeue         bool
		expectPhaseTransition bool
		expectedPhase         v1.CycleNodeRequestPhase
	}{
		{
			name:                  "i/o timeout should requeue (ORIGINAL ISSUE)",
			error:                 errors.New("Post \"https://autoscaling.us-west-2.amazonaws.com/\": dial tcp 18.246.117.35:443: i/o timeout"),
			expectRequeue:         true,
			expectPhaseTransition: false,
			expectedPhase:         v1.CycleNodeRequestPending,
		},
		{
			name:                  "dial tcp timeout should requeue",
			error:                 errors.New("dial tcp: i/o timeout"),
			expectRequeue:         true,
			expectPhaseTransition: false,
			expectedPhase:         v1.CycleNodeRequestPending,
		},
		{
			name:                  "connection refused should requeue",
			error:                 errors.New("dial tcp: connection refused"),
			expectRequeue:         true,
			expectPhaseTransition: false,
			expectedPhase:         v1.CycleNodeRequestPending,
		},
		{
			name:                  "AWS throttling should requeue",
			error:                 awserr.New("Throttling", "Rate exceeded", nil),
			expectRequeue:         true,
			expectPhaseTransition: false,
			expectedPhase:         v1.CycleNodeRequestPending,
		},
		{
			name:                  "AWS ServiceUnavailable should requeue",
			error:                 awserr.New("ServiceUnavailable", "Service temporarily unavailable", nil),
			expectRequeue:         true,
			expectPhaseTransition: false,
			expectedPhase:         v1.CycleNodeRequestPending,
		},
		{
			name:                  "AWS InternalFailure should requeue",
			error:                 awserr.New("InternalFailure", "Internal service error", nil),
			expectRequeue:         true,
			expectPhaseTransition: false,
			expectedPhase:         v1.CycleNodeRequestPending,
		},
		{
			name:                  "AccessDenied should transition to Healing (permanent error)",
			error:                 awserr.New("AccessDenied", "Access denied", nil),
			expectRequeue:         false,
			expectPhaseTransition: true,
			expectedPhase:         v1.CycleNodeRequestHealing,
		},
		{
			name:                  "InvalidParameter should transition to Healing (permanent error)",
			error:                 errors.New("invalid parameter: nodegroup name"),
			expectRequeue:         false,
			expectPhaseTransition: true,
			expectedPhase:         v1.CycleNodeRequestHealing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			cnr := &v1.CycleNodeRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cnr",
					Namespace: "kube-system",
				},
				Spec: v1.CycleNodeRequestSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role": "worker",
						},
					},
					NodeGroupsList: []string{"test-nodegroup"},
					CycleSettings: v1.CycleSettings{
						Method: "Drain",
					},
				},
				Status: v1.CycleNodeRequestStatus{
					Phase: v1.CycleNodeRequestPending,
					EquilibriumWaitStarted: metav1.Time{
						Time: time.Now(),
					},
				},
			}

			// Create mock cloud provider that returns the test error
			mockCP := &mockCloudProviderWithTransientErrors{
				failuresBeforeSuccess: 999, // Always fail in this test
				errorToReturn:         tt.error,
			}

			// Create fake k8s client
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnr).Build()

			// Create resource manager
			rm := &controller.ResourceManager{
				Client:        fakeClient,
				CloudProvider: mockCP,
				Logger:        &testLogger{t: t},
			}

			// Create transitioner
			transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})

			// Execute
			result, err := transitioner.transitionPending()

			// Verify
			if tt.expectRequeue {
				// Should requeue without error
				if err != nil {
					t.Errorf("Expected no error for retryable error, but got: %v", err)
				}
				if !result.Requeue {
					t.Errorf("Expected Requeue=true for retryable error")
				}
				if result.RequeueAfter == 0 {
					t.Errorf("Expected RequeueAfter > 0 for retryable error")
				}
				// Phase should remain Pending
				if cnr.Status.Phase != tt.expectedPhase {
					t.Errorf("Expected phase %s, got %s", tt.expectedPhase, cnr.Status.Phase)
				}
				t.Logf("✓ Retryable error correctly requeued after %v", result.RequeueAfter)
			}

			if tt.expectPhaseTransition {
				// Should transition to Healing (old behavior for permanent errors)
				if cnr.Status.Phase != tt.expectedPhase {
					t.Errorf("Expected phase transition to %s, got %s", tt.expectedPhase, cnr.Status.Phase)
				}
				t.Logf("✓ Permanent error correctly transitioned to %s", cnr.Status.Phase)
			}
		})
	}
}

// TestTransitionPending_RecoveryAfterTransientErrors demonstrates the fix working
// Simulates AWS recovering after several transient failures
func TestTransitionPending_RecoveryAfterTransientErrors(t *testing.T) {
	tests := []struct {
		name                  string
		failuresBeforeSuccess int
		error                 error
		maxAttempts           int
		shouldEventuallySucceed bool
	}{
		{
			name:                  "Recovers after 2 i/o timeouts",
			failuresBeforeSuccess: 2,
			error:                 errors.New("dial tcp: i/o timeout"),
			maxAttempts:           5,
			shouldEventuallySucceed: true,
		},
		{
			name:                  "Recovers after AWS throttling",
			failuresBeforeSuccess: 3,
			error:                 awserr.New("Throttling", "Rate exceeded", nil),
			maxAttempts:           5,
			shouldEventuallySucceed: true,
		},
		{
			name:                  "Does not recover from permanent error",
			failuresBeforeSuccess: 999,
			error:                 awserr.New("AccessDenied", "Access denied", nil),
			maxAttempts:           5,
			shouldEventuallySucceed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup node that matches the selector
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-1",
					Labels: map[string]string{
						"node-role": "worker",
					},
				},
				Spec: corev1.NodeSpec{
					ProviderID: "aws:///us-west-2a/i-1234567890abcdef0",
				},
			}

			cnr := &v1.CycleNodeRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cnr",
					Namespace: "kube-system",
				},
				Spec: v1.CycleNodeRequestSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"node-role": "worker",
						},
					},
					NodeGroupsList: []string{"test-nodegroup"},
					CycleSettings: v1.CycleSettings{
						Method:      "Drain",
						Concurrency: 1,
					},
				},
				Status: v1.CycleNodeRequestStatus{
					Phase: v1.CycleNodeRequestPending,
					EquilibriumWaitStarted: metav1.Time{
						Time: time.Now(),
					},
				},
			}

			// Create mock cloud provider
			mockCP := &mockCloudProviderWithTransientErrors{
				failuresBeforeSuccess: tt.failuresBeforeSuccess,
				errorToReturn:         tt.error,
			}

			// Create fake k8s client
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(cnr, node).
				Build()

			// Create resource manager
			rm := &controller.ResourceManager{
				Client:        fakeClient,
				CloudProvider: mockCP,
				Logger:        &testLogger{t: t},
			}

			// Simulate multiple reconciliation attempts (like controller-runtime would do)
			var lastResult reconcile.Result
			var lastErr error
			recovered := false

			for attempt := 1; attempt <= tt.maxAttempts; attempt++ {
				t.Logf("Attempt %d/%d", attempt, tt.maxAttempts)

				transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})
				lastResult, lastErr = transitioner.transitionPending()

				// Check if we've successfully moved past Pending
				if cnr.Status.Phase != v1.CycleNodeRequestPending {
					if cnr.Status.Phase == v1.CycleNodeRequestInitialised {
						recovered = true
						t.Logf("✓ Successfully recovered after %d attempts", attempt)
						break
					} else if cnr.Status.Phase == v1.CycleNodeRequestHealing {
						t.Logf("✗ Transitioned to Healing (permanent error detected)")
						break
					}
				}

				// If requeue requested, simulate waiting
				if lastResult.Requeue {
					t.Logf("  → Requeued (will retry)")
				}
			}

			// Verify expectations
			if tt.shouldEventuallySucceed {
				if !recovered {
					t.Errorf("Expected recovery after transient errors, but stayed in phase %s after %d attempts",
						cnr.Status.Phase, tt.maxAttempts)
					t.Errorf("Last error: %v", lastErr)
				}
				if mockCP.callCount <= tt.failuresBeforeSuccess {
					t.Errorf("Expected at least %d calls to cloud provider, got %d",
						tt.failuresBeforeSuccess+1, mockCP.callCount)
				}
			} else {
				if recovered {
					t.Errorf("Expected failure for permanent error, but recovered")
				}
				// Permanent errors should transition to Healing immediately (no retries)
				if cnr.Status.Phase != v1.CycleNodeRequestHealing {
					t.Errorf("Expected phase to be Healing for permanent error, got %s", cnr.Status.Phase)
				}
			}
		})
	}
}

// testLogger is a simple logger for tests
type testLogger struct {
	t *testing.T
}

func (l *testLogger) Info(msg string, keysAndValues ...interface{}) {
	l.t.Logf("[INFO] %s %v", msg, keysAndValues)
}

func (l *testLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.t.Logf("[ERROR] %s: %v %v", msg, err, keysAndValues)
}

func (l *testLogger) Enabled() bool {
	return true
}

func (l *testLogger) V(level int) controller.Logger {
	return l
}

func (l *testLogger) WithValues(keysAndValues ...interface{}) controller.Logger {
	return l
}

func (l *testLogger) WithName(name string) controller.Logger {
	return l
}
