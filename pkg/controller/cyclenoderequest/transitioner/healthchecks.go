package transitioner

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"regexp"
	"strings"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// getNodeName generates a unique name for the node by combining the node name and provider ID
// Do not use the provider assigned name of the node
// If the name of the node is the private dns address, there is a risk of new instances being
// assigned the same address as a previously terminated node in the initial set in the nodegroup.
// This would cause the new node health checks to be skipped
func getNodeName(kubeNode corev1.Node) string {
	return fmt.Sprintf("%s/%s", kubeNode.Spec.ProviderID, kubeNode.Name)
}

// buildHealthCheckEndpoint generates the endpoint which will be used to perform a health check
// It will render a string and replace {{ .NodeIP }} with the node private IP. If this is not present,
// then the endpoint returned will be identical to the input
func buildHealthCheckEndpoint(node corev1.Node, endpoint string) (string, error) {
	tmpl, err := template.New("endpoint").Parse(endpoint)
	if err != nil {
		return "", err
	}

	var privateIP string

	// If there is no private IP, the error will be caught when trying
	// to perform the health check on the node
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			privateIP = address.Address
		}
	}

	// Ensure other fields cannot be rendered to the filter
	tmplStruct := struct {
		NodeIP string
	}{
		NodeIP: privateIP,
	}

	var renderedEndpoint strings.Builder
	if err = tmpl.Execute(&renderedEndpoint, tmplStruct); err != nil {
		return "", err
	}

	return renderedEndpoint.String(), nil
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
		return fmt.Errorf("regex %s did not match body %b", healthCheck.RegexMatch, body)
	}

	for _, validStatusCode := range healthCheck.ValidStatusCodes {
		if statusCode == validStatusCode {
			return nil
		}
	}

	return fmt.Errorf("status code %d returned, did not match expected %v", statusCode, healthCheck.ValidStatusCodes)
}

// performHealthCheck makes the health check request to the endpoint specified, reads the body and returns
// the status code/body to determinate weather it passed
func (t *CycleNodeRequestTransitioner) performHealthCheck(endpoint string) (uint, []byte, error) {
	resp, err := t.rm.HttpClient.Get(endpoint)
	if err != nil {
		return 0, nil, err
	}

	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}

	return uint(resp.StatusCode), bytes, nil
}

// performInitialHealthChecks on the nodes selected to be terminated before cycling begin. If any health
// check fails return an error to prevent cycling from starting
func (t *CycleNodeRequestTransitioner) performInitialHealthChecks(kubeNodes []corev1.Node) error {
	t.cycleNodeRequest.Status.HealthChecks = make(map[string]v1.HealthCheckStatus)

	// Build a set of ready nodes from which to check below
	readyNodesSet := make(map[string]corev1.Node)

	// Collect all nodes in the nodegroup and assign Skip=true
	// Health checks will not be performed on them during cycling in the ScalingUp phase
	for _, kubeNode := range kubeNodes {
		nodeName := getNodeName(kubeNode)
		readyNodesSet[nodeName] = kubeNode

		// Set up the health check statuses for each node
		// All nodes present in the nodegroup before cycling should be skipped after cycling has begun
		t.cycleNodeRequest.Status.HealthChecks[nodeName] = v1.HealthCheckStatus{
			Skip: true,
		}
	}

	// Perform healthchecks on all nodes in the nodegroup before cycling begins
	// If any healthchecks fail, cycling should not start
	for _, node := range t.cycleNodeRequest.Status.NodesToTerminate {
		nodeName := fmt.Sprintf("%s/%s", node.ProviderID, node.Name)

		// Check if the node is ready, fail the cnr
		kubeNode, ok := readyNodesSet[nodeName]
		if !ok {
			return fmt.Errorf("node %s/%s not ready", nodeName, node.Name)
		}

		for _, healthCheck := range t.cycleNodeRequest.Spec.HealthChecks {
			endpoint, err := buildHealthCheckEndpoint(kubeNode, healthCheck.Endpoint)
			if err != nil {
				return fmt.Errorf("failed to build health check endpoint: %v", err)
			}

			statusCode, body, err := t.performHealthCheck(endpoint)
			if err != nil {
				return fmt.Errorf("initial health check %s failed: %v", endpoint, err)
			}

			if err := healthCheckPassed(healthCheck, statusCode, body); err != nil {
				return fmt.Errorf("initial health check %s failed: %v", endpoint, err)
			}

			t.rm.Logger.Info("Initial health check passed", "endpoint", endpoint)
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
		nodeName := getNodeName(kubeNode)
		healthChecksStatus, ok := t.cycleNodeRequest.Status.HealthChecks[nodeName]

		// Pick up the new instances attached to the nodegroup
		// All original instances were already added in the Pending phase
		// Do not add set Skip=true or else they will be skipped as part of the health checks below
		if !ok {
			healthChecksStatus = v1.HealthCheckStatus{
				Checks: make([]bool, len(t.cycleNodeRequest.Spec.HealthChecks)),
			}

			t.cycleNodeRequest.Status.HealthChecks[nodeName] = healthChecksStatus
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
			t.cycleNodeRequest.Status.HealthChecks[nodeName] = healthChecksStatus
		}

		for i, healthCheck := range t.cycleNodeRequest.Spec.HealthChecks {
			// If the health check has already passed, skip it
			if healthChecksStatus.Checks[i] {
				continue
			}

			endpoint, err := buildHealthCheckEndpoint(kubeNode, healthCheck.Endpoint)
			if err != nil {
				return false, fmt.Errorf("failed to build health check endpoint: %v", err)
			}

			// If the wait period has been exceeded, the health check is considered to have failed
			if healthChecksStatus.NodeReady.Add(healthCheck.WaitPeriod.Duration).Before(metav1.Now().Time) {
				return false, fmt.Errorf("health check %s failed: didn't become healthy in time", endpoint)
			}

			// Perform the health check and log any error but don't fail the cycle
			// If a workload which is being checked has not started up yet, it should be allowed time to do so
			// as configured in the Nodegroup spec
			statusCode, body, err := t.performHealthCheck(endpoint)
			if err != nil {
				t.rm.Logger.Error(err, "Health check failed", "endpoint", endpoint)
				continue
			}

			// Still within the waiting period here, must trigger requeueing this phase
			// Don't bother logging the error, this would fill up the logs
			if err := healthCheckPassed(healthCheck, statusCode, body); err != nil {
				allHealthChecksPassed = false
				continue
			}

			t.rm.Logger.Info("Health check passed", "endpoint", endpoint)

			healthChecksStatus.Checks[i] = true
			t.cycleNodeRequest.Status.HealthChecks[nodeName] = healthChecksStatus
		}
	}

	return allHealthChecksPassed, nil
}
