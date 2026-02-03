package transitioner

import (
	"errors"
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cloudprovider"
	"github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestOriginalIssue_IOTimeoutCausesHealing reproduces the original bug
// BEFORE FIX: AWS i/o timeout → immediate Healing → Failed
// AFTER FIX: AWS i/o timeout → retry → requeue → eventual success
func TestOriginalIssue_IOTimeoutCausesHealing(t *testing.T) {
	t.Run("BEFORE FIX: i/o timeout transitions to Healing immediately", func(t *testing.T) {
		// This test documents the OLD behavior - it would fail with the fix applied
		// Skip this test since we've fixed the issue
		t.Skip("This test documents old behavior - now fixed")

		cnr := createTestCycleNodeRequest("test-cnr", v1.CycleNodeRequestPending)
		node := createTestNode("test-node-1", "aws:///us-west-2a/i-1234567890abcdef0")

		// Mock cloud provider that always returns i/o timeout
		mockCP := &mockCloudProviderAlwaysFails{
			error: errors.New("Post \"https://autoscaling.us-west-2.amazonaws.com/\": dial tcp 18.246.117.35:443: i/o timeout"),
		}

		rm := createTestResourceManager(t, cnr, node, mockCP)
		transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})

		// Execute
		_, err := transitioner.transitionPending()

		// OLD behavior: would transition to Healing
		if cnr.Status.Phase != v1.CycleNodeRequestHealing {
			t.Errorf("OLD behavior: Expected transition to Healing, got %s", cnr.Status.Phase)
		}
		if err == nil {
			t.Errorf("OLD behavior: Expected error to be returned")
		}
	})

	t.Run("AFTER FIX: i/o timeout requeues for retry", func(t *testing.T) {
		cnr := createTestCycleNodeRequest("test-cnr", v1.CycleNodeRequestPending)
		node := createTestNode("test-node-1", "aws:///us-west-2a/i-1234567890abcdef0")

		// Mock cloud provider that returns i/o timeout
		mockCP := &mockCloudProviderAlwaysFails{
			error: errors.New("Post \"https://autoscaling.us-west-2.amazonaws.com/\": dial tcp 18.246.117.35:443: i/o timeout"),
		}

		rm := createTestResourceManager(t, cnr, node, mockCP)
		transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})

		// Execute
		result, err := transitioner.transitionPending()

		// NEW behavior: should requeue without error
		if err != nil {
			t.Errorf("NEW behavior: Expected no error (requeue instead), got: %v", err)
		}
		if !result.Requeue {
			t.Errorf("NEW behavior: Expected Requeue=true")
		}
		if result.RequeueAfter == 0 {
			t.Errorf("NEW behavior: Expected RequeueAfter > 0")
		}
		if cnr.Status.Phase != v1.CycleNodeRequestPending {
			t.Errorf("NEW behavior: Expected to stay in Pending phase, got %s", cnr.Status.Phase)
		}

		t.Logf("✓ FIX VERIFIED: i/o timeout now requeues after %v instead of failing", result.RequeueAfter)
	})
}

// TestEndToEnd_TransientErrorRecovery demonstrates the complete recovery flow
func TestEndToEnd_TransientErrorRecovery(t *testing.T) {
	scenarios := []struct {
		name                  string
		error                 error
		failuresBeforeSuccess int
		description           string
	}{
		{
			name:                  "i/o timeout recovery (original issue)",
			error:                 errors.New("Post \"https://autoscaling.us-west-2.amazonaws.com/\": dial tcp 18.246.117.35:443: i/o timeout"),
			failuresBeforeSuccess: 3,
			description:           "Network timeout recovers after 3 attempts",
		},
		{
			name:                  "AWS throttling recovery",
			error:                 awserr.New("Throttling", "Rate limit exceeded", nil),
			failuresBeforeSuccess: 2,
			description:           "AWS rate limiting recovers after backoff",
		},
		{
			name:                  "Service unavailable recovery",
			error:                 awserr.New("ServiceUnavailable", "Service temporarily unavailable", nil),
			failuresBeforeSuccess: 4,
			description:           "AWS service outage recovers after retries",
		},
		{
			name:                  "Connection reset recovery",
			error:                 errors.New("read tcp: connection reset by peer"),
			failuresBeforeSuccess: 2,
			description:           "Connection reset recovers after retries",
		},
		{
			name:                  "TLS handshake timeout recovery",
			error:                 errors.New("TLS handshake timeout"),
			failuresBeforeSuccess: 2,
			description:           "TLS timeout recovers after retries",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			cnr := createTestCycleNodeRequest("test-cnr", v1.CycleNodeRequestPending)
			node := createTestNode("test-node-1", "aws:///us-west-2a/i-1234567890abcdef0")

			// Mock cloud provider that fails N times then succeeds
			mockCP := &mockCloudProviderWithTransientErrors{
				failuresBeforeSuccess: scenario.failuresBeforeSuccess,
				errorToReturn:         scenario.error,
			}

			rm := createTestResourceManager(t, cnr, node, mockCP)

			// Simulate controller reconciliation loop
			maxAttempts := scenario.failuresBeforeSuccess + 5
			recovered := false

			for attempt := 1; attempt <= maxAttempts; attempt++ {
				transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})
				result, err := transitioner.transitionPending()

				t.Logf("Attempt %d: Phase=%s, Requeue=%v, Error=%v",
					attempt, cnr.Status.Phase, result.Requeue, err)

				// Check if we've recovered (moved to Initialised phase)
				if cnr.Status.Phase == v1.CycleNodeRequestInitialised {
					recovered = true
					t.Logf("✓ RECOVERED: %s after %d attempts", scenario.description, attempt)
					break
				}

				// Should not transition to Healing/Failed for transient errors
				if cnr.Status.Phase == v1.CycleNodeRequestHealing || cnr.Status.Phase == v1.CycleNodeRequestFailed {
					t.Errorf("❌ FAILED: Transitioned to %s for transient error (old behavior)", cnr.Status.Phase)
					break
				}

				// Should stay in Pending and requeue
				if cnr.Status.Phase != v1.CycleNodeRequestPending {
					t.Errorf("Expected Pending phase during retries, got %s", cnr.Status.Phase)
					break
				}
			}

			if !recovered {
				t.Errorf("❌ FAILED: Did not recover after %d attempts", maxAttempts)
			}

			// Verify cloud provider was called enough times
			if mockCP.callCount <= scenario.failuresBeforeSuccess {
				t.Errorf("Expected at least %d cloud provider calls, got %d",
					scenario.failuresBeforeSuccess+1, mockCP.callCount)
			}
		})
	}
}

// TestEndToEnd_PermanentErrorsStillFail verifies permanent errors still fail correctly
func TestEndToEnd_PermanentErrorsStillFail(t *testing.T) {
	permanentErrors := []struct {
		name  string
		error error
	}{
		{
			name:  "AccessDenied",
			error: awserr.New("AccessDenied", "Access denied", nil),
		},
		{
			name:  "InvalidParameter",
			error: awserr.New("ValidationError", "Invalid parameter", nil),
		},
		{
			name:  "InvalidInstanceID",
			error: awserr.New("InvalidInstanceID.NotFound", "Instance not found", nil),
		},
	}

	for _, tc := range permanentErrors {
		t.Run(tc.name, func(t *testing.T) {
			cnr := createTestCycleNodeRequest("test-cnr", v1.CycleNodeRequestPending)
			node := createTestNode("test-node-1", "aws:///us-west-2a/i-1234567890abcdef0")

			// Mock cloud provider that always returns permanent error
			mockCP := &mockCloudProviderAlwaysFails{
				error: tc.error,
			}

			rm := createTestResourceManager(t, cnr, node, mockCP)
			transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})

			// Execute
			result, err := transitioner.transitionPending()

			// Permanent errors should transition to Healing (not requeue)
			if cnr.Status.Phase != v1.CycleNodeRequestHealing {
				t.Errorf("Expected Healing phase for permanent error, got %s", cnr.Status.Phase)
			}
			if err == nil {
				t.Errorf("Expected error to be returned for permanent error")
			}
			if result.Requeue {
				t.Errorf("Expected no requeue for permanent error")
			}

			t.Logf("✓ Permanent error correctly transitioned to Healing: %s", tc.error)
		})
	}
}

// TestEndToEnd_EquilibriumTimeout verifies timeout still works
func TestEndToEnd_EquilibriumTimeout(t *testing.T) {
	t.Run("Transitions to Healing after equilibrium timeout", func(t *testing.T) {
		cnr := createTestCycleNodeRequest("test-cnr", v1.CycleNodeRequestPending)
		node := createTestNode("test-node-1", "aws:///us-west-2a/i-1234567890abcdef0")

		// Set equilibrium wait to past the timeout
		cnr.Status.EquilibriumWaitStarted = &metav1.Time{
			Time: time.Now().Add(-10 * time.Minute), // Past the 5 minute limit
		}

		mockCP := &mockCloudProviderAlwaysFails{
			error: errors.New("i/o timeout"), // Transient error
		}

		rm := createTestResourceManager(t, cnr, node, mockCP)
		transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})

		// Execute
		_, err := transitioner.transitionPending()

		// Should transition to Healing due to timeout (not because of error type)
		if cnr.Status.Phase != v1.CycleNodeRequestHealing {
			t.Errorf("Expected Healing phase after equilibrium timeout, got %s", cnr.Status.Phase)
		}
		if err == nil {
			t.Errorf("Expected error to be returned")
		}

		t.Logf("✓ Equilibrium timeout still works correctly")
	})
}

// TestEndToEnd_CompareBeforeAndAfter creates a side-by-side comparison
func TestEndToEnd_CompareBeforeAndAfter(t *testing.T) {
	t.Run("Side-by-side comparison of behavior", func(t *testing.T) {
		testError := errors.New("Post \"https://autoscaling.us-west-2.amazonaws.com/\": dial tcp 18.246.117.35:443: i/o timeout")

		t.Log("=== BEHAVIOR COMPARISON ===")
		t.Log("")
		t.Log("Scenario: AWS API returns i/o timeout during GetNodeGroups")
		t.Log("")

		// Test current (fixed) behavior
		t.Log("AFTER FIX:")
		cnr := createTestCycleNodeRequest("test-cnr-after", v1.CycleNodeRequestPending)
		node := createTestNode("test-node-1", "aws:///us-west-2a/i-1234567890abcdef0")
		mockCP := &mockCloudProviderAlwaysFails{error: testError}
		rm := createTestResourceManager(t, cnr, node, mockCP)
		transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})

		result, err := transitioner.transitionPending()

		t.Logf("  Phase: %s → %s", v1.CycleNodeRequestPending, cnr.Status.Phase)
		t.Logf("  Error returned: %v", err)
		t.Logf("  Requeue: %v", result.Requeue)
		t.Logf("  RequeueAfter: %v", result.RequeueAfter)
		t.Logf("  Result: Will retry automatically ✓")
		t.Log("")

		// Verify new behavior
		if cnr.Status.Phase != v1.CycleNodeRequestPending {
			t.Errorf("Expected to stay in Pending phase, got %s", cnr.Status.Phase)
		}
		if err != nil {
			t.Errorf("Expected no error (should requeue), got %v", err)
		}
		if !result.Requeue {
			t.Errorf("Expected requeue for retry")
		}

		t.Log("SUMMARY:")
		t.Log("  ✓ Transient errors now trigger automatic retry")
		t.Log("  ✓ CNR stays in Pending phase instead of transitioning to Healing")
		t.Log("  ✓ Controller will automatically requeue and retry")
		t.Log("  ✓ System can recover from intermittent network issues")
	})
}

// Helper functions

func createTestCycleNodeRequest(name string, phase v1.CycleNodeRequestPhase) *v1.CycleNodeRequest {
	return &v1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
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
			Phase: phase,
			EquilibriumWaitStarted: &metav1.Time{
				Time: time.Now(),
			},
		},
	}
}

func createTestNode(name, providerID string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"node-role": "worker",
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
	}
}

func createTestResourceManager(t *testing.T, cnr *v1.CycleNodeRequest, node *corev1.Node, cp cloudprovider.CloudProvider) *controller.ResourceManager {
	scheme := runtime.NewScheme()
	_ = v1.SchemeBuilder.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cnr, node).
		Build()

	return &controller.ResourceManager{
		Client:        fakeClient,
		CloudProvider: cp,
		Logger:        testr.NewWithOptions(t, testr.Options{Verbosity: 10}),
	}
}

// mockCloudProviderAlwaysFails always returns the configured error
type mockCloudProviderAlwaysFails struct {
	error error
}

func (m *mockCloudProviderAlwaysFails) GetNodeGroups(names []string) (cloudprovider.NodeGroups, error) {
	return nil, m.error
}

func (m *mockCloudProviderAlwaysFails) InstancesExist(providerIDs []string) (map[string]interface{}, error) {
	return nil, m.error
}

func (m *mockCloudProviderAlwaysFails) TerminateInstance(providerID string) error {
	return m.error
}

func (m *mockCloudProviderAlwaysFails) Name() string {
	return "mock-aws-failing"
}

// mockCloudProviderWithTransientErrors simulates AWS transient failures
type mockCloudProviderWithTransientErrors struct {
	callCount             int
	failuresBeforeSuccess int
	errorToReturn         error
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

func (m *mockCloudProviderWithTransientErrors) Name() string {
	return "mock-aws"
}

// mockNodeGroups is a simple mock implementation
type mockNodeGroups struct{}

func (m *mockNodeGroups) Instances() map[string]cloudprovider.Instance {
	return map[string]cloudprovider.Instance{
		"aws:///us-west-2a/i-1234567890abcdef0": &mockInstance{
			id:         "i-1234567890abcdef0",
			providerID: "aws:///us-west-2a/i-1234567890abcdef0",
		},
	}
}

func (m *mockNodeGroups) DetachInstance(providerID string) (bool, error) {
	return false, nil
}

func (m *mockNodeGroups) AttachInstance(providerID, nodeGroup string) (bool, error) {
	return false, nil
}

func (m *mockNodeGroups) ReadyInstances() map[string]cloudprovider.Instance {
	return m.Instances()
}

func (m *mockNodeGroups) NotReadyInstances() map[string]cloudprovider.Instance {
	return make(map[string]cloudprovider.Instance)
}

type mockInstance struct {
	id         string
	providerID string
}

func (m *mockInstance) ID() string {
	return m.id
}

func (m *mockInstance) OutOfDate() bool {
	return false
}

func (m *mockInstance) MatchesProviderID(providerID string) bool {
	return m.providerID == providerID
}

func (m *mockInstance) NodeGroupName() string {
	return "test-nodegroup"
}
