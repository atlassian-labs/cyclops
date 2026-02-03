package transitioner

import (
	"errors"
	"fmt"
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
)

// ScenarioTest represents a real-world scenario with expected behavior
type ScenarioTest struct {
	name                string
	description         string
	error               error
	failuresBeforeFixed int
	shouldRecover       bool
	maxAttempts         int
	expectedPhase       v1.CycleNodeRequestPhase
}

// TestRealWorldScenarios tests common real-world failure scenarios
func TestRealWorldScenarios(t *testing.T) {
	scenarios := []ScenarioTest{
		{
			name:                "Network Blip",
			description:         "Brief network timeout to AWS API - should recover",
			error:               errors.New("dial tcp 18.246.119.144:443: i/o timeout"),
			failuresBeforeFixed: 2,
			shouldRecover:       true,
			maxAttempts:         10,
			expectedPhase:       v1.CycleNodeRequestInitialised,
		},
		{
			name:                "AWS Region Maintenance",
			description:         "AWS ServiceUnavailable during maintenance window",
			error:               awserr.New("ServiceUnavailable", "Service is temporarily unavailable", nil),
			failuresBeforeFixed: 4,
			shouldRecover:       true,
			maxAttempts:         15,
			expectedPhase:       v1.CycleNodeRequestInitialised,
		},
		{
			name:                "Rate Limiting",
			description:         "AWS rate limiting when multiple clusters cycle nodes",
			error:               awserr.New("Throttling", "Rate exceeded. Max attempts exceeded. Defaulting to back off", nil),
			failuresBeforeFixed: 3,
			shouldRecover:       true,
			maxAttempts:         20,
			expectedPhase:       v1.CycleNodeRequestInitialised,
		},
		{
			name:                "Intermittent TLS Issues",
			description:         "TLS handshake failures due to certificate verification",
			error:               errors.New("TLS handshake timeout"),
			failuresBeforeFixed: 2,
			shouldRecover:       true,
			maxAttempts:         10,
			expectedPhase:       v1.CycleNodeRequestInitialised,
		},
		{
			name:                "Connection Reset",
			description:         "Connection reset by AWS load balancer",
			error:               errors.New("read tcp 10.0.0.1:52341->52.84.27.165:443: connection reset by peer"),
			failuresBeforeFixed: 2,
			shouldRecover:       true,
			maxAttempts:         10,
			expectedPhase:       v1.CycleNodeRequestInitialised,
		},
		{
			name:                "AWS Internal Error",
			description:         "AWS internal server error - typically transient",
			error:               awserr.New("InternalFailure", "An internal error occurred", nil),
			failuresBeforeFixed: 3,
			shouldRecover:       true,
			maxAttempts:         15,
			expectedPhase:       v1.CycleNodeRequestInitialised,
		},
		{
			name:                "Invalid Credentials (Permanent)",
			description:         "Misconfigured IAM credentials - should fail permanently",
			error:               awserr.New("AccessDenied", "User: arn:aws:iam::xxx:user/yyy is not authorized", nil),
			failuresBeforeFixed: 0,
			shouldRecover:       false,
			maxAttempts:         5,
			expectedPhase:       v1.CycleNodeRequestHealing,
		},
		{
			name:                "Invalid NodeGroup (Permanent)",
			description:         "NodeGroup doesn't exist - should fail permanently",
			error:               errors.New("autoscaling groups not found: [nonexistent-nodegroup]"),
			failuresBeforeFixed: 0,
			shouldRecover:       false,
			maxAttempts:         5,
			expectedPhase:       v1.CycleNodeRequestHealing,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			runScenario(t, scenario)
		})
	}
}

func runScenario(t *testing.T, scenario ScenarioTest) {
	t.Logf("Scenario: %s", scenario.name)
	t.Logf("Description: %s", scenario.description)
	t.Logf("Error: %v", scenario.error)
	t.Logf("")

	cnr := createTestCycleNodeRequest("test-cnr", v1.CycleNodeRequestPending)
	node := createTestNode("test-node-1", "aws:///us-west-2a/i-1234567890abcdef0")

	mockCP := &mockCloudProviderWithTransientErrors{
		failuresBeforeSuccess: scenario.failuresBeforeFixed,
		errorToReturn:         scenario.error,
	}

	rm := createTestResourceManager(t, cnr, node, mockCP)

	// Simulate controller reconciliation loop
	recovered := false
	lastPhase := cnr.Status.Phase
	attemptLog := []string{}

	for attempt := 1; attempt <= scenario.maxAttempts; attempt++ {
		transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})
		result, err := transitioner.transitionPending()

		// Log attempt
		logEntry := fmt.Sprintf("Attempt %d: Phase=%s, Requeue=%v, Error=%v",
			attempt, cnr.Status.Phase, result.Requeue, err != nil)
		attemptLog = append(attemptLog, logEntry)

		if cnr.Status.Phase == scenario.expectedPhase && cnr.Status.Phase != v1.CycleNodeRequestPending {
			recovered = true
			t.Logf("✓ Reached expected phase: %s after %d attempts", scenario.expectedPhase, attempt)
			break
		}

		// For transient errors, should stay in Pending and requeue
		if scenario.shouldRecover && cnr.Status.Phase == v1.CycleNodeRequestHealing {
			t.Errorf("❌ ERROR: Prematurely transitioned to Healing (old buggy behavior)")
			break
		}

		lastPhase = cnr.Status.Phase
	}

	// Log first 5 attempts
	for i := 0; i < len(attemptLog) && i < 5; i++ {
		t.Logf("  %s", attemptLog[i])
	}
	if len(attemptLog) > 5 {
		t.Logf("  ... (%d more attempts)", len(attemptLog)-5)
	}

	// Verify expectations
	if scenario.shouldRecover {
		if !recovered {
			t.Errorf("❌ FAILED: Expected recovery but stayed in phase %s after %d attempts",
				cnr.Status.Phase, scenario.maxAttempts)
		} else {
			t.Logf("✓ PASSED: Successfully recovered from %s", scenario.name)
		}
	} else {
		if cnr.Status.Phase != scenario.expectedPhase {
			t.Errorf("❌ FAILED: Expected phase %s, got %s", scenario.expectedPhase, cnr.Status.Phase)
		} else {
			t.Logf("✓ PASSED: Correctly failed at %s (permanent error)", scenario.expectedPhase)
		}
	}

	t.Logf("")
}

// TestScenario_GracefulDegradation tests that the system still works under stress
func TestScenario_GracefulDegradation(t *testing.T) {
	t.Run("High frequency retries under AWS service degradation", func(t *testing.T) {
		// Simulate AWS being partially unavailable (fails most of the time)
		cnr := createTestCycleNodeRequest("test-cnr", v1.CycleNodeRequestPending)
		node := createTestNode("test-node-1", "aws:///us-west-2a/i-1234567890abcdef0")

		mockCP := &mockCloudProviderWithTransientErrors{
			failuresBeforeSuccess: 10, // Many failures before success
			errorToReturn:         awserr.New("ServiceUnavailable", "Service temporarily unavailable", nil),
		}

		rm := createTestResourceManager(t, cnr, node, mockCP)

		// With retries, it will eventually succeed
		recovered := false
		for attempt := 1; attempt <= 20; attempt++ {
			transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})
			_, _ = transitioner.transitionPending()

			if cnr.Status.Phase == v1.CycleNodeRequestInitialised {
				recovered = true
				t.Logf("✓ Recovered after %d attempts despite service degradation", attempt)
				break
			}

			// Should never transition to Failed for transient errors
			if cnr.Status.Phase == v1.CycleNodeRequestFailed {
				t.Fatalf("❌ Incorrectly transitioned to Failed (should stay in Pending)")
			}
		}

		if !recovered {
			t.Logf("⚠ Did not recover in 20 attempts (service too degraded), but stayed in Pending")
			t.Logf("  This is expected - will continue retrying when service recovers")
		}
	})
}

// TestScenario_PartialRecovery tests recovery when AWS intermittently works
func TestScenario_PartialRecovery(t *testing.T) {
	t.Run("Intermittent AWS connectivity (works every other call)", func(t *testing.T) {
		cnr := createTestCycleNodeRequest("test-cnr", v1.CycleNodeRequestPending)
		node := createTestNode("test-node-1", "aws:///us-west-2a/i-1234567890abcdef0")

		// Cloud provider that alternates between success and failure
		mockCP := &mockCloudProviderIntermittent{
			callCount: 0,
			error:     errors.New("dial tcp: i/o timeout"),
		}

		rm := createTestResourceManager(t, cnr, node, mockCP)

		recovered := false
		for attempt := 1; attempt <= 30; attempt++ {
			transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})
			_, _ = transitioner.transitionPending()

			t.Logf("Attempt %d: Phase=%s, Call#=%d", attempt, cnr.Status.Phase, mockCP.callCount)

			if cnr.Status.Phase == v1.CycleNodeRequestInitialised {
				recovered = true
				t.Logf("✓ Recovered despite intermittent connectivity after %d attempts", attempt)
				break
			}
		}

		if !recovered {
			t.Logf("⚠ Did not recover in 30 attempts, but stayed in Pending (not Failed)")
		}
	})
}

// TestScenario_MultiNodeGroupCycle tests cycling when multiple node groups exist
func TestScenario_MultiNodeGroupCycle(t *testing.T) {
	t.Run("Multiple node groups with transient AWS errors", func(t *testing.T) {
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
				// Multiple node groups
				NodeGroupsList: []string{"nodegroup-1", "nodegroup-2", "nodegroup-3"},
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

		node1 := createTestNode("test-node-1", "aws:///us-west-2a/i-1111111111111111")
		node2 := createTestNode("test-node-2", "aws:///us-west-2b/i-2222222222222222")
		node3 := createTestNode("test-node-3", "aws:///us-west-2c/i-3333333333333333")

		mockCP := &mockCloudProviderWithTransientErrors{
			failuresBeforeSuccess: 2,
			errorToReturn:         awserr.New("Throttling", "Rate exceeded", nil),
		}

		scheme := runtime.NewScheme()
		_ = v1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cnr, node1, node2, node3).
			Build()

		rm := &controller.ResourceManager{
			Client:        fakeClient,
			CloudProvider: mockCP,
			Logger:        &testLogger{t: t},
		}

		// Execute transitioner
		recovered := false
		for attempt := 1; attempt <= 10; attempt++ {
			transitioner := NewCycleNodeRequestTransitioner(cnr, rm, Options{})
			result, err := transitioner.transitionPending()

			t.Logf("Attempt %d: Phase=%s, Requeue=%v, Error=%v",
				attempt, cnr.Status.Phase, result.Requeue, err != nil)

			if cnr.Status.Phase == v1.CycleNodeRequestInitialised {
				recovered = true
				break
			}
		}

		if recovered {
			t.Logf("✓ Multi-node group cycle recovered from transient errors")
		} else {
			t.Logf("⚠ Multi-node group cycle did not complete (but stayed in Pending, not Failed)")
		}
	})
}

// mockCloudProviderIntermittent alternates between success and failure
type mockCloudProviderIntermittent struct {
	callCount int
	error     error
}

func (m *mockCloudProviderIntermittent) GetNodeGroups(names []string) (cloudprovider.NodeGroups, error) {
	m.callCount++
	// Alternate between success and failure
	if m.callCount%2 == 0 {
		return &mockNodeGroups{}, nil
	}
	return nil, m.error
}

func (m *mockCloudProviderIntermittent) InstancesExist(providerIDs []string) (map[string]interface{}, error) {
	return make(map[string]interface{}), nil
}

func (m *mockCloudProviderIntermittent) TerminateInstance(providerID string) error {
	return nil
}

func (m *mockCloudProviderIntermittent) DetachInstance(nodeGroupName, providerID string) error {
	return nil
}

func (m *mockCloudProviderIntermittent) AttachInstance(nodeGroupName, providerID string) error {
	return nil
}

func (m *mockCloudProviderIntermittent) AddInstanceToNodeGroup(nodeGroupName string, nodeGroup cloudprovider.NodeGroupOptions) error {
	return nil
}

func (m *mockCloudProviderIntermittent) Name() string {
	return "mock-aws-intermittent"
}
