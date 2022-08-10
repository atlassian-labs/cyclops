package transitioner

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// getNodeHash generates a unique name for the node by combining the node name and provider ID
// Do not use the provider assigned name of the node
// If the name of the node is the private dns address, there is a risk of new instances being
// assigned the same address as a previously terminated node in the initial set in the nodegroup.
// This would cause the new node health checks to be skipped
func getNodeHash(node v1.CycleNodeRequestNode) string {
	return fmt.Sprintf("%s/%s", node.ProviderID, node.Name)
}

// getCycleRequestNode converts the node object to v1.CycleNodeRequestNode so it can be used
// across health checks and pre-termination checks
func getCycleRequestNode(kubeNode corev1.Node) v1.CycleNodeRequestNode {
	var privateIP string

	// If there is no private IP, the error will be caught when trying
	// to perform the health check on the node
	for _, address := range kubeNode.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			privateIP = address.Address
		}
	}

	return v1.CycleNodeRequestNode{
		Name:       kubeNode.Name,
		ProviderID: kubeNode.Spec.ProviderID,
		PrivateIP:  privateIP,
	}
}

// buildHealthCheckEndpoint generates the endpoint which will be used to perform a health check
// It will render a string and replace {{ .NodeIP }} with the node private IP. If this is not present,
// then the endpoint returned will be identical to the input
func buildHealthCheckEndpoint(node v1.CycleNodeRequestNode, endpoint string) (string, error) {
	tmpl, err := template.New("endpoint").Parse(endpoint)
	if err != nil {
		return "", err
	}

	// Ensure other fields cannot be rendered to the filter
	tmplStruct := struct {
		NodeIP string
	}{
		NodeIP: node.PrivateIP,
	}

	var renderedEndpoint strings.Builder
	if err = tmpl.Execute(&renderedEndpoint, tmplStruct); err != nil {
		return "", err
	}

	return renderedEndpoint.String(), nil
}

// Build a http client which contains the root CA and certs configured as environment
// variables. The environment variables have already been validated, need to check
// again in here.
func (t *CycleNodeRequestTransitioner) buildHttpClient(tlsConfig v1.TLSConfig) (*http.Client, error) {
	config := &tls.Config{}

	rootCA, ok := os.LookupEnv(tlsConfig.RootCA)
	if ok {
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM([]byte(rootCA))
		config.RootCAs = caCertPool
	}

	// Both will be either configured or missing
	certificate, ok := os.LookupEnv(tlsConfig.Certificate)
	key, _ := os.LookupEnv(tlsConfig.Key)

	if ok {
		cert, err := tls.X509KeyPair([]byte(certificate), []byte(key))
		if err != nil {
			return nil, fmt.Errorf("failed to load certs for client: %v", err)
		}

		config.Certificates = []tls.Certificate{cert}
	}

	// Return the configured client and add the timeout from the "default" client
	return &http.Client{
		Timeout: t.rm.HttpClient.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: config,
		},
	}, nil
}

// healthCheckPassed checks if the statusCode returned matches the set of valid status code for the health check
// as well as if the body matches regex provided
func healthCheckPassed(healthCheck v1.HealthCheck, statusCode uint, body []byte) error {
	// If there is not regex match string specified, don't check the body of the response
	r, err := regexp.Compile(healthCheck.RegexMatch)
	if err != nil {
		return err
	}

	if healthCheck.RegexMatch != "" && !r.Match(body) {
		return fmt.Errorf("regex %s did not match body %s", healthCheck.RegexMatch, string(body))
	}

	for _, validStatusCode := range healthCheck.ValidStatusCodes {
		if statusCode == validStatusCode {
			return nil
		}
	}

	return fmt.Errorf("status code %d returned, did not match expected %v", statusCode, healthCheck.ValidStatusCodes)
}

// makeRequest makes the health check request to the endpoint specified, reads the body and returns
// the status code/body to determinate weather it passed
func (t *CycleNodeRequestTransitioner) makeRequest(httpMethod string, httpClient *http.Client, endpoint string) (uint, []byte, error) {
	httpReq, err := http.NewRequest(httpMethod, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return 0, nil, err
	}

	defer resp.Body.Close()

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}

	return uint(resp.StatusCode), bytes, nil
}

// performHealthCheck builds the endpoint, checks that the waiting period han't been exceeded and then makes the
// request for the health checks. It will finally return whether the health check passed.
func (t *CycleNodeRequestTransitioner) performHealthCheck(node v1.CycleNodeRequestNode, healthCheck v1.HealthCheck, anchorTime *metav1.Time) (bool, error) {
	endpoint, err := buildHealthCheckEndpoint(node, healthCheck.Endpoint)
	if err != nil {
		return false, fmt.Errorf("failed to build health check endpoint: %v", err)
	}

	// If the wait period has been exceeded, the health check is considered to have failed
	// Only perform this check if the anchor time is supplied
	if anchorTime != nil && anchorTime.Add(healthCheck.WaitPeriod.Duration).Before(metav1.Now().Time) {
		return false, fmt.Errorf("health check %s failed: didn't become healthy in time", endpoint)
	}

	httpClient, err := t.buildHttpClient(healthCheck.TLSConfig)
	if err != nil {
		return false, fmt.Errorf("failed to build http client: %v", err)
	}

	// Perform the health check and log any error but don't fail the cycle
	// If a workload which is being checked has not started up yet, it should be allowed time to do so
	// as configured in the Nodegroup spec
	statusCode, body, err := t.makeRequest(http.MethodGet, httpClient, endpoint)
	if err != nil {
		t.rm.Logger.Error(err, "Health check failed", "endpoint", endpoint, "error", err)
		return true, fmt.Errorf("health check failed: %v", err)
	}

	// Still within the waiting period here, must trigger requeueing this phase
	if err := healthCheckPassed(healthCheck, statusCode, body); err != nil {
		return true, fmt.Errorf("health check did not pass for the endpoint %s, got: (%d) %s", endpoint, statusCode, string(body))
	}

	t.rm.Logger.Info("Health check passed", "endpoint", endpoint)
	return true, nil
}

// performInitialHealthChecks on the nodes selected to be terminated before cycling begin. If any health
// check fails return an error to prevent cycling from starting
func (t *CycleNodeRequestTransitioner) performInitialHealthChecks(kubeNodes []corev1.Node) error {
	// Build a set of ready nodes from which to check below
	readyNodesSet := make(map[string]v1.CycleNodeRequestNode)

	// Collect all nodes in the nodegroup and assign Skip=true
	// Health checks will not be performed on them during cycling in the ScalingUp phase
	for _, kubeNode := range kubeNodes {
		node := getCycleRequestNode(kubeNode)
		nodeHash := getNodeHash(node)

		readyNodesSet[nodeHash] = node

		// Set up the health check statuses for each node
		// All nodes present in the nodegroup before cycling should be skipped after cycling has begun
		t.cycleNodeRequest.Status.HealthChecks[nodeHash] = v1.HealthCheckStatus{
			Skip: true,
		}
	}

	// The CNR can be configured to skip the initial set of health checks
	// This can be useful for cycling out known unhealthy nodes which would fail these checks
	if t.cycleNodeRequest.Spec.SkipInitialHealthChecks {
		t.rm.Logger.Info("Skipping initial health checks")
		return nil
	}

	// Perform healthchecks on all nodes in the nodegroup before cycling begins
	// If any healthchecks fail, cycling should not start
	for _, node := range t.cycleNodeRequest.Status.NodesToTerminate {
		nodeHash := getNodeHash(node)

		// Check if the node is ready, fail the cnr
		_, ok := readyNodesSet[nodeHash]
		if !ok {
			return fmt.Errorf("node %s/%s not ready", nodeHash, node.Name)
		}

		for _, healthCheck := range t.cycleNodeRequest.Spec.HealthChecks {
			// Perform the health check on the instance without an anchor time, the health check
			// should immediately pass, if it doesn't then fail the CNR because that's an issue.
			// As a result, disregard whether the error is allowed or not.
			if _, err := t.performHealthCheck(node, healthCheck, nil); err != nil {
				return fmt.Errorf("initial: %v", err)
			}
		}
	}

	return nil
}

// performCyclingHealthChecks before terminating an instance selected for termination. Cycling pauses
// until all health checks pass for the new instance before terminating the old one
func (t *CycleNodeRequestTransitioner) performCyclingHealthChecks(kubeNodes []corev1.Node) (bool, error) {
	var allHealthChecksPassed bool = true

	// Find new instsances attached to the nodegroup and perform health checks on them
	// before terminating the old ones they are replacing
	// Cycling is paused until all health checks pass
	// If no health checks are configured, nothing happens
	for _, kubeNode := range kubeNodes {
		node := getCycleRequestNode(kubeNode)
		nodeHash := getNodeHash(node)

		healthChecksStatus, ok := t.cycleNodeRequest.Status.HealthChecks[nodeHash]

		// Pick up the new instances attached to the nodegroup
		// All original instances were already added in the Pending phase
		// Do not add set Skip=true or else they will be skipped as part of the health checks below
		if !ok {
			healthChecksStatus = v1.HealthCheckStatus{
				Checks: make([]bool, len(t.cycleNodeRequest.Spec.HealthChecks)),
			}

			t.cycleNodeRequest.Status.HealthChecks[nodeHash] = healthChecksStatus
		}

		// This is a node which was part of the nodegroup before cycling began
		// Skip it and move on to the next one
		if healthChecksStatus.Skip {
			continue
		}

		// At this point, a new ready node has been identified
		// Attempt the configured health checks

		if healthChecksStatus.NodeReady == nil {
			now := metav1.Now()
			healthChecksStatus.NodeReady = &now
			t.cycleNodeRequest.Status.HealthChecks[nodeHash] = healthChecksStatus
		}

		for i, healthCheck := range t.cycleNodeRequest.Spec.HealthChecks {
			// If the health check has already passed, skip it
			if healthChecksStatus.Checks[i] {
				continue
			}

			errorAllowed, err := t.performHealthCheck(node, healthCheck, healthChecksStatus.NodeReady)

			// If the error is not allowed then the cycling should fail
			if !errorAllowed && err != nil {
				return false, fmt.Errorf("cycling: %v", err)
			}

			// If the error is allowed, log out the error and continue to the next health check
			if err != nil {
				allHealthChecksPassed = false
				continue
			}

			// Update after each check passes in case the next one returns an error
			healthChecksStatus.Checks[i] = true
			t.cycleNodeRequest.Status.HealthChecks[nodeHash] = healthChecksStatus
		}
	}

	return allHealthChecksPassed, nil
}

// sendPreTerminationTrigger sends a http request as a trigger. When this is done, the upstream host
// will know that the associated node is going to be terminated and so it should begin it's own
// shutdown process before that begins. This can be thought of as a http sigterm.
func (t *CycleNodeRequestTransitioner) sendPreTerminationTrigger(node v1.CycleNodeRequestNode) error {
	nodeHash := getNodeHash(node)

	// The first time this runs, the status will not have been initialised yet
	if t.cycleNodeRequest.Status.PreTerminationChecks == nil {
		t.cycleNodeRequest.Status.PreTerminationChecks = make(map[string]v1.PreTerminationCheckStatusList)
	}

	status, ok := t.cycleNodeRequest.Status.PreTerminationChecks[nodeHash]
	if !ok {
		status = v1.PreTerminationCheckStatusList{
			Checks: make([]v1.PreTerminationCheckStatus, len(t.cycleNodeRequest.Spec.PreTerminationChecks)),
		}

		t.cycleNodeRequest.Status.PreTerminationChecks[nodeHash] = status
	}

	for i, preTerminationCheck := range t.cycleNodeRequest.Spec.PreTerminationChecks {
		// If the trigger has already been sent, skip this
		// This is a common case and will happen each time before health checks are
		// performed
		if status.Checks[i].Trigger != nil {
			continue
		}

		endpoint, err := buildHealthCheckEndpoint(node, preTerminationCheck.Endpoint)
		if err != nil {
			return fmt.Errorf("failed to build health check endpoint: %v", err)
		}

		httpClient, err := t.buildHttpClient(preTerminationCheck.TLSConfig)
		if err != nil {
			return fmt.Errorf("failed to build http client: %v", err)
		}

		// Send the trigger, disregard the response body
		statusCode, _, err := t.makeRequest(http.MethodPost, httpClient, endpoint)
		if err != nil {
			return fmt.Errorf("sending trigger failed: %v", err)
		}

		var statusCodeFound bool

		for _, validStatusCode := range preTerminationCheck.ValidStatusCodes {
			if statusCode == validStatusCode {
				statusCodeFound = true
			}
		}

		if !statusCodeFound {
			return fmt.Errorf("got unexpected status code after sending trigger: %d", statusCode)
		}

		now := metav1.Now()
		status.Checks[i].Trigger = &now
		t.cycleNodeRequest.Status.PreTerminationChecks[nodeHash] = status
	}

	return nil
}

// performPreTerminationHealthChecks is a health check performed on the upstream server after the trigger has been sent.
// It monitors the progress shutdown progress. Cyclops will wait until this endpoint returns the expected response before
// proceeding to terminate the node.
func (t *CycleNodeRequestTransitioner) performPreTerminationHealthChecks(node v1.CycleNodeRequestNode) (bool, error) {
	var allHealthChecksPassed bool = true
	nodeHash := getNodeHash(node)

	// Check that the trigger has already been send to the node before performing any health checks
	// If this not the case then an error should be returned as this is not expected
	// The trigger must already be sent before or else Cyclops would be stuck in an loop until
	// the wait time runs out
	preTerminationCheckStatus, ok := t.cycleNodeRequest.Status.PreTerminationChecks[nodeHash]
	if !ok {
		return false, fmt.Errorf("trying to perform a health check on a node before the trigger")
	}

	// Perform all the health checks configured on the node
	for i, preTerminationCheck := range t.cycleNodeRequest.Spec.PreTerminationChecks {
		status := preTerminationCheckStatus.Checks[i]

		// If the health check has already passed, skip it
		if status.Check {
			continue
		}

		errorAllowed, err := t.performHealthCheck(node, preTerminationCheck.HealthCheck, status.Trigger)
		// If the error is not allowed then the cycling should fail
		if !errorAllowed && err != nil {
			return false, fmt.Errorf("pre-termination: %v", err)
		}

		// If the error is allowed, log out the error and continue to the next health check
		if err != nil {
			allHealthChecksPassed = false
			continue
		}

		// Update after each check passes in case the next one returns an error
		status.Check = true
		preTerminationCheckStatus.Checks[i] = status
		t.cycleNodeRequest.Status.PreTerminationChecks[nodeHash] = preTerminationCheckStatus
	}

	return allHealthChecksPassed, nil
}
