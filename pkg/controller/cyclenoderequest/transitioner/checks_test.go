package transitioner

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/controller"
	"github.com/go-logr/logr"     // required for the resource manager logger
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// getNodeHash function tests, verifies the node hash is generated correctly
func TestChecks_GetNodeHash(t *testing.T) {
	tests := []struct {
		name     string
		node     v1.CycleNodeRequestNode
		expected string
	}{
		{
			name: "basic node hash, expected output",
			node: v1.CycleNodeRequestNode{
				Name:       "node-1",
				ProviderID: "aws:///us-east-1a/i-1234567890abcdef0",
			},
			expected: "aws:///us-east-1a/i-1234567890abcdef0/node-1",
		},
		{
			name: "empty provider id, expected output",
			node: v1.CycleNodeRequestNode{
				Name:       "node-1",
				ProviderID: "",
			},
			expected: "/node-1",
		},
		{
			name: "empty name, expected output",
			node: v1.CycleNodeRequestNode{
				Name:       "",
				ProviderID: "aws:///us-east-1a/i-1234567890abcdef0",
			},
			expected: "aws:///us-east-1a/i-1234567890abcdef0/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getNodeHash(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// getCycleRequestNode function tests, verifies the node object is converted to v1.CycleNodeRequestNode correctly
func TestChecks_GetCycleRequestNode(t *testing.T) {
	tests := []struct {
		name     string
		kubeNode corev1.Node
		expected v1.CycleNodeRequestNode
	}{
		{
			name: "node with internal IP",
			kubeNode: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-1",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "aws:///us-east-1a/i-1234567890abcdef0",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},     // internal IP is used
						{Type: corev1.NodeExternalIP, Address: "54.1.2.3"},     // external IP is ignored
					},
				},
			},
			expected: v1.CycleNodeRequestNode{
				Name:       "node-1",
				ProviderID: "aws:///us-east-1a/i-1234567890abcdef0",
				PrivateIP:  "10.0.0.1",
			},
		},
		{
			name: "node without internal IP",
			kubeNode: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-2",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "aws:///us-east-1b/i-0987654321fedcba0",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeExternalIP, Address: "54.1.2.3"},     // external IP is ignored
					},
				},
			},
			expected: v1.CycleNodeRequestNode{
				Name:       "node-2",
				ProviderID: "aws:///us-east-1b/i-0987654321fedcba0",
				PrivateIP:  "",   // no internal IP found, private IP is empty
			},
		},
		{
			name: "node with multiple internal IPs uses last one",
			kubeNode: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-3",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "aws:///us-east-1c/i-abcdef1234567890",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},     // first internal IP is ignored
						{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},     // second internal IP is used
					},
				},
			},
			expected: v1.CycleNodeRequestNode{
				Name:       "node-3",
				ProviderID: "aws:///us-east-1c/i-abcdef1234567890",
				PrivateIP:  "10.0.0.2",
			},
		},
		{
			name: "node with no addresses",
			kubeNode: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-4",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "aws:///us-east-1d/i-fedcba0987654321",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{},   // no addresses found
				},
			},
			expected: v1.CycleNodeRequestNode{
				Name:       "node-4",
				ProviderID: "aws:///us-east-1d/i-fedcba0987654321",
				PrivateIP:  "",   // no internal IP found, private IP is empty
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCycleRequestNode(tt.kubeNode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// buildHealthCheckEndpoint function tests, verifies the endpoint is generated correctly
func TestChecks_BuildHealthCheckEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		node        v1.CycleNodeRequestNode
		endpoint    string
		expected    string
		expectError bool
	}{
		{
			name: "endpoint with NodeIP template",
			node: v1.CycleNodeRequestNode{
				Name:      "node-1",
				PrivateIP: "10.0.0.1",
			},
			endpoint:    "http://{{ .NodeIP }}:8080/health",   // template is used to replace the node private IP
			expected:    "http://10.0.0.1:8080/health",
			expectError: false,
		},
		{
			name: "endpoint without template",
			node: v1.CycleNodeRequestNode{
				Name:      "node-1",
				PrivateIP: "10.0.0.1",
			},
			endpoint:    "http://example.com/health",   // template is not used, endpoint is returned as is
			expected:    "http://example.com/health",
			expectError: false,
		},
		{
			name: "endpoint with multiple NodeIP occurrences",
			node: v1.CycleNodeRequestNode{
				Name:      "node-1",
				PrivateIP: "192.168.1.100",
			},
			endpoint:    "http://{{ .NodeIP }}:8080/check?host={{ .NodeIP }}",   // .NodeIP is used twice
			expected:    "http://192.168.1.100:8080/check?host=192.168.1.100",
			expectError: false,
		},
		{
			name: "invalid template syntax",
			node: v1.CycleNodeRequestNode{
				Name:      "node-1",
				PrivateIP: "10.0.0.1",
			},
			endpoint:    "http://{{ .NodeIP }:8080/health",   // missing closing brace
			expected:    "",
			expectError: true,
		},
		{
			name: "empty endpoint",
			node: v1.CycleNodeRequestNode{
				Name:      "node-1",
				PrivateIP: "10.0.0.1",
			},
			endpoint:    "",   // empty endpoint is allowed
			expected:    "",
			expectError: false,
		},
		{
			name: "node with empty private IP",
			node: v1.CycleNodeRequestNode{
				Name:      "node-1",
				PrivateIP: "",   // private IP is empty
			},
			endpoint:    "http://{{ .NodeIP }}:8080/health",
			expected:    "http://:8080/health",   // empty .NodeIP is replaced with empty string
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildHealthCheckEndpoint(tt.node, tt.endpoint)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// healthCheckPassed function tests, verifies the status code and body are matched correctly
func TestChecks_HealthCheckPassed(t *testing.T) {
	tests := []struct {
		name        string
		healthCheck v1.HealthCheck
		statusCode  uint
		body        []byte
		expectError bool
		errorMsg    string
	}{
		{
			name: "matching status code, no regex",
			healthCheck: v1.HealthCheck{
				ValidStatusCodes: []uint{200},
				RegexMatch:       "",   // no regex match is configured
			},
			statusCode:  200,
			body:        []byte("OK"),
			expectError: false,
		},
		{
			name: "matching status code from multiple valid codes",
			healthCheck: v1.HealthCheck{
				ValidStatusCodes: []uint{200, 201, 204}, 
				RegexMatch:       "",
			},
			statusCode:  201,   // 201 is a valid status code
			body:        []byte("Created successfully"),
			expectError: false,
		},
		{
			name: "non-matching status code",
			healthCheck: v1.HealthCheck{
				ValidStatusCodes: []uint{200},
				RegexMatch:       "",
			},
			statusCode:  500,   // 500 is not in ValidStatusCodes
			body:        []byte("Internal Server Error"),
			expectError: true,
			errorMsg:    "status code 500 returned, did not match expected [200]",
		},
		{
			name: "matching status code and matching regex",
			healthCheck: v1.HealthCheck{
				ValidStatusCodes: []uint{200},
				RegexMatch:       "healthy",   // regex match is configured
			},
			statusCode:  200,
			body:        []byte(`{"status": "healthy"}`),   // contains the string "healthy"
			expectError: false,
		},
		{
			name: "matching status code but non-matching regex",
			healthCheck: v1.HealthCheck{
				ValidStatusCodes: []uint{200},
				RegexMatch:       "^healthy$",   // exact match is configured
			},
			statusCode:  200,
			body:        []byte(`{"status": "unhealthy"}`),   // no exact match for "healthy"
			expectError: true,
			errorMsg:    `regex ^healthy$ did not match body {"status": "unhealthy"}`,
		},
		{
			name: "invalid regex pattern",
			healthCheck: v1.HealthCheck{
				ValidStatusCodes: []uint{200},
				RegexMatch:       "[invalid",   // invalid regex pattern
			},
			statusCode:  200,
			body:        []byte("OK"),
			expectError: true,
		},
		{
			name: "complex regex pattern",
			healthCheck: v1.HealthCheck{
				ValidStatusCodes: []uint{200},
				RegexMatch:       `"status":\s*"(ready|healthy)"`,   // can be either ready or healthy
			},
			statusCode:  200,
			body:        []byte(`{"status": "ready"}`),
			expectError: false,
		},
		{
			name: "empty valid status codes",
			healthCheck: v1.HealthCheck{
				ValidStatusCodes: []uint{},   // no valid status codes configured
				RegexMatch:       "",
			},
			statusCode:  200,
			body:        []byte("OK"),
			expectError: true,
			errorMsg:    "status code 200 returned, did not match expected []",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := healthCheckPassed(tt.healthCheck, tt.statusCode, tt.body)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// makeRequest function tests, verifies the request is made correctly
func TestChecks_MakeRequest(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.HandlerFunc
		httpMethod     string
		expectedStatus uint
		expectedBody   string
		expectError    bool
	}{
		{
			name: "successful GET request",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				w.WriteHeader(http.StatusOK)   // mock 200 response code
				_, _ = w.Write([]byte(`{"status": "healthy"}`))
			},
			httpMethod:     http.MethodGet,
			expectedStatus: 200,
			expectedBody:   `{"status": "healthy"}`,
			expectError:    false,
		},
		{
			name: "successful POST request",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				w.WriteHeader(http.StatusAccepted)   // mock 202 response code
				_, _ = w.Write([]byte("accepted"))
			},
			httpMethod:     http.MethodPost,
			expectedStatus: 202,
			expectedBody:   "accepted",
			expectError:    false,
		},
		{
			name: "server returns 500",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)   // mock 500 response code
				_, _ = w.Write([]byte("internal error"))
			},
			httpMethod:     http.MethodGet,
			expectedStatus: 500,
			expectedBody:   "internal error",
			expectError:    false,
		},
		{
			name: "empty response body",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)   // mock 204 response code
			},
			httpMethod:     http.MethodGet,
			expectedStatus: 204,
			expectedBody:   "",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			transitioner := &CycleNodeRequestTransitioner{
				rm: &controller.ResourceManager{
					HttpClient: &http.Client{Timeout: 5 * time.Second},
				},
			}

			statusCode, body, err := transitioner.makeRequest(tt.httpMethod, server.Client(), server.URL)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedStatus, statusCode)
				assert.Equal(t, tt.expectedBody, string(body))
			}
		})
	}
}

// makeRequest function test with connection error, verifies the connection error is returned
func TestChecks_MakeRequest_ConnectionError(t *testing.T) {
	transitioner := &CycleNodeRequestTransitioner{
		rm: &controller.ResourceManager{
			HttpClient: &http.Client{Timeout: 1 * time.Second},
		},
	}

	// Try to connect to a non-existent server
	_, _, err := transitioner.makeRequest(http.MethodGet, &http.Client{Timeout: 1 * time.Second}, "http://localhost:59999/nonexistent")   // non-existent server
	assert.Error(t, err)
}

// performHealthCheck function tests, verifies the health check is performed correctly
func TestChecks_PerformHealthCheck(t *testing.T) {
	tests := []struct {
		name           string
		node           v1.CycleNodeRequestNode
		healthCheck    v1.HealthCheck
		anchorTime     *metav1.Time
		serverHandler  http.HandlerFunc
		expectContinue bool
		expectError    bool
	}{
		{
			name: "health check passes",
			node: v1.CycleNodeRequestNode{
				Name:      "node-1",
				PrivateIP: "127.0.0.1",
			},
			healthCheck: v1.HealthCheck{
				Endpoint:         "PLACEHOLDER",
				ValidStatusCodes: []uint{200},
				WaitPeriod:       &metav1.Duration{Duration: 10 * time.Minute},
			},
			anchorTime: nil,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)   // mock 200 response code
				_, _ = w.Write([]byte("healthy"))
			},
			expectContinue: true,
			expectError:    false,
		},
		{
			name: "health check fails with wrong status",
			node: v1.CycleNodeRequestNode{
				Name:      "node-1",
				PrivateIP: "127.0.0.1",
			},
			healthCheck: v1.HealthCheck{
				Endpoint:         "PLACEHOLDER",
				ValidStatusCodes: []uint{200},
				WaitPeriod:       &metav1.Duration{Duration: 10 * time.Minute},
			},
			anchorTime: nil,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)   // mock 503 response code
				_, _ = w.Write([]byte("not ready"))
			},
			expectContinue: true,
			expectError:    true,
		},
		{
			name: "wait period exceeded",
			node: v1.CycleNodeRequestNode{
				Name:      "node-1",
				PrivateIP: "127.0.0.1",
			},
			healthCheck: v1.HealthCheck{
				Endpoint:         "PLACEHOLDER",
				ValidStatusCodes: []uint{200},
				WaitPeriod:       &metav1.Duration{Duration: 1 * time.Millisecond}, // 1 ms wait period
			},
			anchorTime: func() *metav1.Time {
				t := metav1.NewTime(time.Now().Add(-1 * time.Hour))   // anchor time is 1 hour in the past to simulate the wait period being exceeded
				return &t
			}(),
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)   // mock 200 response code, should be ignored as wait period is exceeded
			},
			expectContinue: false,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.serverHandler)
			defer server.Close()

			// Update endpoint to use test server URL
			tt.healthCheck.Endpoint = server.URL

			cnr := &v1.CycleNodeRequest{
				Spec: v1.CycleNodeRequestSpec{
					HealthChecks: []v1.HealthCheck{tt.healthCheck},
				},
			}

			transitioner := &CycleNodeRequestTransitioner{
				cycleNodeRequest: cnr,
				rm: &controller.ResourceManager{
					HttpClient: &http.Client{Timeout: 5 * time.Second},
					Logger:     logr.Discard(),
				},
			}

			continueProcessing, err := transitioner.performHealthCheck(tt.node, tt.healthCheck, tt.anchorTime)

			assert.Equal(t, tt.expectContinue, continueProcessing)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// performInitialHealthChecks function tests, verifies the initial health checks are performed correctly on the nodes selected to be terminated before cycling begin
func TestChecks_PerformInitialHealthChecks(t *testing.T) {
	tests := []struct {
		name                    string
		kubeNodes               map[string]corev1.Node
		nodesToTerminate        []v1.CycleNodeRequestNode
		healthChecks            []v1.HealthCheck
		skipInitialHealthChecks bool
		serverHandler           http.HandlerFunc
		expectError             bool
	}{
		{
			name:                    "skip initial health checks",
			kubeNodes:               map[string]corev1.Node{},
			nodesToTerminate:        []v1.CycleNodeRequestNode{},
			healthChecks:            []v1.HealthCheck{},
			skipInitialHealthChecks: true,    // skip initial health checks
			serverHandler:           nil,
			expectError:             false,
		},
		{
			name: "no health checks configured, no errors expected",
			kubeNodes: map[string]corev1.Node{
				"aws:///us-east-1a/i-123": {
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Spec:       corev1.NodeSpec{ProviderID: "aws:///us-east-1a/i-123"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
						},
					},
				},
			},
			nodesToTerminate: []v1.CycleNodeRequestNode{
				{Name: "node-1", ProviderID: "aws:///us-east-1a/i-123", PrivateIP: "10.0.0.1"},   // node-1 is in readyNodesSet (reads from kubeNodes)
			},
			healthChecks:            []v1.HealthCheck{},   // no health checks configured
			skipInitialHealthChecks: false,
			serverHandler:           nil,
			expectError:             false,
		},
		{
			name: "node is not in readyNodesSet, error expected",
			kubeNodes: map[string]corev1.Node{
				"aws:///us-east-1a/i-123": {
					ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
					Spec:       corev1.NodeSpec{ProviderID: "aws:///us-east-1a/i-123"},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
						},
					},
				},
			},
			nodesToTerminate: []v1.CycleNodeRequestNode{
				{Name: "node-2", ProviderID: "aws:///us-east-1a/i-456", PrivateIP: "10.0.0.2"}, // node-2 is not in readyNodesSet (reads from kubeNodes)
			},
			healthChecks:            []v1.HealthCheck{},
			skipInitialHealthChecks: false,
			serverHandler:           nil,
			expectError:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server
			if tt.serverHandler != nil {
				server = httptest.NewServer(tt.serverHandler)
				defer server.Close()

				// Update endpoints to use test server URL
				for i := range tt.healthChecks {
					tt.healthChecks[i].Endpoint = server.URL
				}
			}

			cnr := &v1.CycleNodeRequest{
				Spec: v1.CycleNodeRequestSpec{
					HealthChecks:            tt.healthChecks,
					SkipInitialHealthChecks: tt.skipInitialHealthChecks,
				},
				Status: v1.CycleNodeRequestStatus{
					NodesToTerminate: tt.nodesToTerminate,
					HealthChecks:     make(map[string]v1.HealthCheckStatus),
				},
			}

			transitioner := &CycleNodeRequestTransitioner{
				cycleNodeRequest: cnr,
				rm: &controller.ResourceManager{
					HttpClient: &http.Client{Timeout: 5 * time.Second},
					Logger:     logr.Discard(),
				},
			}

			err := transitioner.performInitialHealthChecks(tt.kubeNodes)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// performCyclingHealthChecks function tests, verifies the cycling health checks are performed correctly on the new nodes
func TestChecks_PerformCyclingHealthChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)   // mock 200 response code
		_, _ = w.Write([]byte("healthy"))
	}))
	defer server.Close()

	healthCheck := v1.HealthCheck{
		Endpoint:         server.URL,
		ValidStatusCodes: []uint{200},
		WaitPeriod:       &metav1.Duration{Duration: 10 * time.Minute},
	}

	t.Run("new node health check passes", func(t *testing.T) {
		kubeNodes := map[string]corev1.Node{
			"aws:///us-east-1a/i-new": {
				ObjectMeta: metav1.ObjectMeta{Name: "new-node"},
				Spec:       corev1.NodeSpec{ProviderID: "aws:///us-east-1a/i-new"},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.0.2"},
					},
				},
			},
		}

		cnr := &v1.CycleNodeRequest{
			Spec: v1.CycleNodeRequestSpec{
				HealthChecks: []v1.HealthCheck{healthCheck},
			},
			Status: v1.CycleNodeRequestStatus{
				HealthChecks: make(map[string]v1.HealthCheckStatus),
			},
		}

		transitioner := &CycleNodeRequestTransitioner{
			cycleNodeRequest: cnr,
			rm: &controller.ResourceManager{
				HttpClient: &http.Client{Timeout: 5 * time.Second},
				Logger:     logr.Discard(),
			},
		}

		allPassed, err := transitioner.performCyclingHealthChecks(kubeNodes) // should pass as mock 200 response code is returned
		assert.NoError(t, err)
		assert.True(t, allPassed)
	})

	t.Run("skip node with Skip flag", func(t *testing.T) {
		kubeNodes := map[string]corev1.Node{
			"aws:///us-east-1a/i-old": {
				ObjectMeta: metav1.ObjectMeta{Name: "old-node"},
				Spec:       corev1.NodeSpec{ProviderID: "aws:///us-east-1a/i-old"},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
					},
				},
			},
		}

		cnr := &v1.CycleNodeRequest{
			Spec: v1.CycleNodeRequestSpec{
				HealthChecks: []v1.HealthCheck{healthCheck},
			},
			Status: v1.CycleNodeRequestStatus{
				HealthChecks: map[string]v1.HealthCheckStatus{
					"aws:///us-east-1a/i-old/old-node": {Skip: true},   // old-node is skipped as Skip flag is set
				},
			},
		}

		transitioner := &CycleNodeRequestTransitioner{
			cycleNodeRequest: cnr,
			rm: &controller.ResourceManager{
				HttpClient: &http.Client{Timeout: 5 * time.Second},
				Logger:     logr.Discard(),
			},
		}

		allPassed, err := transitioner.performCyclingHealthChecks(kubeNodes) // should pass as old-node is skipped
		assert.NoError(t, err)
		assert.True(t, allPassed)
	})
}

// sendPreTerminationTrigger function tests, verifies the pre-termination trigger is sent correctly
func TestChecks_SendPreTerminationTrigger(t *testing.T) {
	t.Run("trigger sent successfully", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusAccepted)   // mock 202 response code

		}))
		defer server.Close()

		preTerminationCheck := v1.PreTerminationCheck{
			Endpoint:         server.URL,
			ValidStatusCodes: []uint{202},
			HealthCheck: v1.HealthCheck{
				Endpoint:         server.URL + "/health",
				ValidStatusCodes: []uint{200},
				WaitPeriod:       &metav1.Duration{Duration: 10 * time.Minute},
			},
		}

		cnr := &v1.CycleNodeRequest{ 
			Spec: v1.CycleNodeRequestSpec{
				PreTerminationChecks: []v1.PreTerminationCheck{preTerminationCheck},
			},
			Status: v1.CycleNodeRequestStatus{},
		}

		transitioner := &CycleNodeRequestTransitioner{
			cycleNodeRequest: cnr,
			rm: &controller.ResourceManager{
				HttpClient: &http.Client{Timeout: 5 * time.Second},
				Logger:     logr.Discard(),
			},
		}

		node := v1.CycleNodeRequestNode{   // node-1 is the node to send the pre-termination trigger to
			Name:       "node-1",
			ProviderID: "aws:///us-east-1a/i-123",
			PrivateIP:  "10.0.0.1",
		}

		err := transitioner.sendPreTerminationTrigger(node) // should pass as mock 202 response code is returned
		assert.NoError(t, err)

		// Verify status was updated
		nodeHash := "aws:///us-east-1a/i-123/node-1"
		status, ok := cnr.Status.PreTerminationChecks[nodeHash]   // status is updated by the transitioner
		assert.True(t, ok)
		assert.NotNil(t, status.Checks[0].Trigger)
	})

	t.Run("trigger fails with wrong status code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)  // mock 500 response code
		}))
		defer server.Close()

		preTerminationCheck := v1.PreTerminationCheck{
			Endpoint:         server.URL,
			ValidStatusCodes: []uint{202},
			HealthCheck: v1.HealthCheck{
				Endpoint:         server.URL + "/health",
				ValidStatusCodes: []uint{200},
				WaitPeriod:       &metav1.Duration{Duration: 10 * time.Minute},
			},
		}

		cnr := &v1.CycleNodeRequest{
			Spec: v1.CycleNodeRequestSpec{
				PreTerminationChecks: []v1.PreTerminationCheck{preTerminationCheck},
			},
			Status: v1.CycleNodeRequestStatus{},
		}

		transitioner := &CycleNodeRequestTransitioner{
			cycleNodeRequest: cnr,
			rm: &controller.ResourceManager{
				HttpClient: &http.Client{Timeout: 5 * time.Second},
				Logger:     logr.Discard(),
			},
		}

		node := v1.CycleNodeRequestNode{ 
			Name:       "node-1",
			ProviderID: "aws:///us-east-1a/i-123",
			PrivateIP:  "10.0.0.1",
		}

		err := transitioner.sendPreTerminationTrigger(node) // should fail as mock 500 response code is returned
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "got unexpected status code")
	})
}

// performPreTerminationHealthChecks function tests, verifies the pre-termination health checks are performed correctly
func TestChecks_PerformPreTerminationHealthChecks(t *testing.T) {
	t.Run("trigger not sent returns error", func(t *testing.T) {
		cnr := &v1.CycleNodeRequest{
			Spec: v1.CycleNodeRequestSpec{
				PreTerminationChecks: []v1.PreTerminationCheck{},
			},
			Status: v1.CycleNodeRequestStatus{
				PreTerminationChecks: make(map[string]v1.PreTerminationCheckStatusList),  // no pre-termination checks in status yet
			},
		}

		transitioner := &CycleNodeRequestTransitioner{
			cycleNodeRequest: cnr,
			rm: &controller.ResourceManager{
				HttpClient: &http.Client{Timeout: 5 * time.Second},
				Logger:     logr.Discard(),
			},
		}

		node := v1.CycleNodeRequestNode{
			Name:       "node-1",
			ProviderID: "aws:///us-east-1a/i-123",
			PrivateIP:  "10.0.0.1",
		}

		_, err := transitioner.performPreTerminationHealthChecks(node)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "trying to perform a health check on a node before the trigger")
	})

	t.Run("health check passes after trigger", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)   // mock 200 response code
			_, _ = w.Write([]byte("ready")) 
		}))
		defer server.Close()

		now := metav1.Now()
		preTerminationCheck := v1.PreTerminationCheck{
			Endpoint:         server.URL,
			ValidStatusCodes: []uint{200},
			HealthCheck: v1.HealthCheck{
				Endpoint:         server.URL,
				ValidStatusCodes: []uint{200},
				WaitPeriod:       &metav1.Duration{Duration: 10 * time.Minute},
			},
		}

		cnr := &v1.CycleNodeRequest{
			Spec: v1.CycleNodeRequestSpec{
				PreTerminationChecks: []v1.PreTerminationCheck{preTerminationCheck},
			},
			Status: v1.CycleNodeRequestStatus{
				PreTerminationChecks: map[string]v1.PreTerminationCheckStatusList{   // node-1 has pre-termination checks in status 
					"aws:///us-east-1a/i-123/node-1": {
						Checks: []v1.PreTerminationCheckStatus{
							{Trigger: &now, Check: false},   // check status is false initially
						},
					},
				},
			},
		}

		transitioner := &CycleNodeRequestTransitioner{
			cycleNodeRequest: cnr,
			rm: &controller.ResourceManager{
				HttpClient: &http.Client{Timeout: 5 * time.Second},
				Logger:     logr.Discard(),
			},
		}

		node := v1.CycleNodeRequestNode{
			Name:       "node-1",
			ProviderID: "aws:///us-east-1a/i-123",
			PrivateIP:  "10.0.0.1",
		}

		allPassed, err := transitioner.performPreTerminationHealthChecks(node) // should pass as mock 200 response code is returned
		assert.NoError(t, err)
		assert.True(t, allPassed)

		// Verify the check was marked as passed
		nodeHash := "aws:///us-east-1a/i-123/node-1"
		status := cnr.Status.PreTerminationChecks[nodeHash]   // status is updated by the transitioner
		assert.True(t, status.Checks[0].Check)
	})
}
