package generation

import (
	"strings"
	"testing"
	"time"

	"github.com/atlassian-labs/cyclops/pkg/test"

	"github.com/stretchr/testify/assert"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGiveReason(t *testing.T) {
	var cnr atlassianv1.CycleNodeRequest
	GiveReason(&cnr, "test reason 123")
	assert.NotNil(t, cnr.Annotations)
	assert.Equal(t, "test reason 123", cnr.Annotations[cnrReasonAnnotationKey])

	cnr.Annotations["other"] = "test"
	GiveReason(&cnr, "new test reason ABC")
	assert.NotNil(t, cnr.Annotations)
	assert.Equal(t, "new test reason ABC", cnr.Annotations[cnrReasonAnnotationKey])
	assert.Equal(t, "test", cnr.Annotations["other"])
}

func TestUseGenerateNameCNR(t *testing.T) {
	var cnr atlassianv1.CycleNodeRequest
	cnr.Name = "test"
	UseGenerateNameCNR(&cnr)
	assert.Equal(t, "", cnr.Name)
	assert.Equal(t, "test-", cnr.GenerateName)
}

func TestApplyCNR(t *testing.T) {
	selector, _ := metav1.ParseToLabelSelector("test=me")

	var nodeGroup atlassianv1.NodeGroup
	nodeGroup.Name = "system"
	nodeGroup.Spec.NodeGroupName = "system.nodegroup"
	nodeGroup.Spec.NodeSelector = *selector
	nodeGroup.Spec.CycleSettings = atlassianv1.CycleSettings{
		Method:      "Drain",
		Concurrency: 1,
	}
	var ingressGroup atlassianv1.NodeGroup
	ingressGroup.Name = "ingress"
	ingressGroup.Spec.NodeGroupsList = []string{"ingress-us-west-2a", "ingress-us-west-2b", "ingress-us-west-2c"}
	ingressGroup.Spec.NodeSelector = *selector
	ingressGroup.Spec.CycleSettings = atlassianv1.CycleSettings{
		Method:      "Drain",
		Concurrency: 1,
	}

	tests := []struct {
		testName  string
		nodeGroup atlassianv1.NodeGroup
		nodes     []string
		name      string
		namespace string
	}{
		{
			"with name test",
			nodeGroup,
			[]string{"test node"},
			"test",
			"kube-system",
		},
		{
			"without name test",
			nodeGroup,
			[]string{"test node"},
			"",
			"kube-system",
		},
		{
			"without name using node group list test",
			nodeGroup,
			[]string{"test node1", "test node2"},
			"",
			"kube-system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			cnr := GenerateCNR(tt.nodeGroup, tt.nodes, tt.name, tt.namespace)
			if tt.name == "" {
				assert.Equal(t, tt.nodeGroup.Name, cnr.Name)
				assert.Nil(t, cnr.Labels)
			} else {
				assert.Equal(t, tt.name+"-"+tt.nodeGroup.Name, cnr.Name)
				assert.NotNil(t, cnr.Labels)
				assert.Equal(t, tt.name, cnr.Labels[cnrNameLabelKey])
			}
			assert.Equal(t, tt.namespace, cnr.Namespace)
			assert.Equal(t, tt.nodes, cnr.Spec.NodeNames)
			assert.Equal(t, tt.nodeGroup.Spec.NodeSelector, cnr.Spec.Selector)
			assert.ElementsMatch(t, tt.nodeGroup.GetNodeGroupNames(), cnr.GetNodeGroupNames())
			assert.Equal(t, tt.nodeGroup.Spec.CycleSettings, cnr.Spec.CycleSettings)
		})
	}
}

func TestValidateCNR(t *testing.T) {
	nodes := test.BuildTestNodes(10, test.NodeOpts{
		LabelKey:   "select",
		LabelValue: "me",
	})
	var names []string
	for _, node := range nodes {
		names = append(names, node.Name)
	}

	selectorMeta, _ := metav1.ParseToLabelSelector("select=me")
	var nodeGroup atlassianv1.NodeGroup
	nodeGroup.Name = "system"
	nodeGroup.Spec.NodeGroupName = "system.nodegroup"
	nodeGroup.Spec.NodeSelector = *selectorMeta
	nodeGroup.Spec.CycleSettings = atlassianv1.CycleSettings{
		Method:         "Drain",
		Concurrency:    1,
		CyclingTimeout: nil,
	}

	tests := []struct {
		name           string
		nodes          []*v1.Node
		nodeNames      []string
		concurrency    int64
		cyclingTimeout *metav1.Duration
		ok             bool
		reason         string
	}{
		{
			"ok-test",
			nodes,
			names,
			1,
			nil,
			true,
			"",
		},
		{
			"bad-name-test-",
			nodes,
			names,
			1,
			nil,
			false,
			`label value is not valid: a lowercase RFC 1123 label must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character (e.g. 'my-name',  or '123-abc', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?')`,
		},
		{
			"long-test-" + strings.Repeat("a", 256),
			nodes,
			names,
			1,
			nil,
			false,
			"name is not valid: must be no more than 253 characters",
		},
		{
			"test-0-c",
			nodes,
			names,
			0,
			nil,
			false,
			concurrencyEqualsZeroMessage,
		},
		{
			"test-negative-c",
			nodes,
			names,
			-1,
			nil,
			false,
			concurrencyLessThanZeroMessage,
		},
		{
			"test-missing",
			nodes,
			append(names, "missing"),
			1,
			nil,
			false,
			`the node "missing" does not exist in the nodegroup but it is specified to cycle`,
		},
		{
			"test-scaled-0",
			nil,
			nil,
			1,
			nil,
			false,
			nodeGroupScaledToZeroMessage,
		},
		{
			"test-positive-cyclingtimeout",
			nodes,
			names,
			1,
			&metav1.Duration{Duration: 1 * time.Hour},
			true,
			"",
		},
		{
			"test-negative-cyclingtimeout",
			nodes,
			names,
			1,
			&metav1.Duration{Duration: -1 * time.Hour},
			false,
			cyclingTimeoutLessThanZeroMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeLister := test.NewTestNodeWatcher(tt.nodes, test.NodeListerOptions{ReturnErrorOnList: false})
			nodeGroup.Spec.CycleSettings.Concurrency = tt.concurrency
			nodeGroup.Spec.CycleSettings.CyclingTimeout = tt.cyclingTimeout
			cnr := GenerateCNR(nodeGroup, tt.nodeNames, tt.name, "kube-system")
			ok, reason := ValidateCNR(nodeLister, cnr)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.reason, reason)
		})
	}
}
