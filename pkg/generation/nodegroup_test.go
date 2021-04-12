package generation

import (
	"strings"
	"testing"

	"github.com/atlassian-labs/cyclops/pkg/test"
	v1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/assert"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateNodeGroup(t *testing.T) {
	nodes := test.BuildTestNodes(10, test.NodeOpts{
		LabelKey:   "select",
		LabelValue: "me",
	})
	var names []string
	for _, node := range nodes {
		names = append(names, node.Name)
	}

	tests := []struct {
		name           string
		nodes          []*v1.Node
		nodeNames      []string
		concurrency    int64
		cyclingTimeout string
		ok             bool
		reason         string
	}{
		{
			"ok-test",
			nodes,
			names,
			1,
			"3h",
			true,
			"",
		},
		{
			"bad-name-test-",
			nodes,
			names,
			1,
			"3h",
			false,
			`name is not valid: a DNS-1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')`,
		},
		{
			"long-test-" + strings.Repeat("a", 256),
			nodes,
			names,
			1,
			"3h",
			false,
			"name is not valid: must be no more than 253 characters",
		},
		{
			"test-0-c",
			nodes,
			names,
			0,
			"3h",
			false,
			concurrencyEqualsZeroMessage,
		},
		{
			"test-negative-c",
			nodes,
			names,
			-1,
			"3h",
			false,
			concurrencyLessThanZeroMessage,
		},
		{
			"test-does-not-check-missing",
			nodes,
			append(names, "missing"),
			1,
			"3h",
			true,
			"",
		},
		{
			"test-scaled-0",
			nil,
			nil,
			1,
			"3h",
			false,
			nodeGroupScaledToZeroMessage,
		},
		{
			"test-wrongformat-cyclingtimeout",
			nodes,
			names,
			0,
			"1dwj1",
			false,
			concurrencyEqualsZeroMessage,
		},
		{
			"test-negative-cyclingtimeout",
			nodes,
			names,
			-1,
			"-1s",
			false,
			concurrencyLessThanZeroMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeLister := test.NewTestNodeWatcher(tt.nodes, test.NodeListerOptions{ReturnErrorOnList: false})
			selectorMeta, _ := metav1.ParseToLabelSelector("select=me")
			var nodeGroup atlassianv1.NodeGroup
			nodeGroup.Name = tt.name
			nodeGroup.Spec.NodeGroupName = "system.nodegroup"
			nodeGroup.Spec.NodeSelector = *selectorMeta
			nodeGroup.Spec.CycleSettings = atlassianv1.CycleSettings{
				Method:         "Drain",
				Concurrency:    tt.concurrency,
				CyclingTimeout: tt.cyclingTimeout,
			}
			ok, reason := ValidateNodeGroup(nodeLister, nodeGroup)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.reason, reason)
		})
	}
}
