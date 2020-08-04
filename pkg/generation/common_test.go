package generation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/test"
)

func TestGetName(t *testing.T) {
	tests := []struct {
		name         string
		generateName string
		expect       string
	}{
		{
			"name",
			"",
			"name",
		},
		{
			"name",
			"generateName",
			"name",
		},
		{
			"",
			"generateName",
			"generateName",
		},
		{
			"",
			"",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			assert.Equal(t, tt.expect, GetName(metav1.ObjectMeta{
				Name:         tt.name,
				GenerateName: tt.generateName,
			}))
		})
	}
}

func TestGetNameExample(t *testing.T) {
	tests := []struct {
		name         string
		generateName string
		expectA      string
		expectB      string
	}{
		{
			"name",
			"",
			"name",
			"",
		},
		{
			"name",
			"generateName",
			"name",
			"",
		},
		{
			"",
			"generateName",
			"generateName",
			generateExample,
		},
		{
			"",
			"",
			"",
			generateExample,
		},
	}

	for _, tt := range tests {
		t.Run(tt.expectA, func(t *testing.T) {
			a, b := GetNameExample(metav1.ObjectMeta{
				Name:         tt.name,
				GenerateName: tt.generateName,
			})
			assert.Equal(t, tt.expectA, a)
			assert.Equal(t, tt.expectB, b)
		})
	}
}

func TestValidateCycleSettings(t *testing.T) {
	tests := []struct {
		name          string
		cycleSettings atlassianv1.CycleSettings
		ok            bool
		reason        string
	}{
		{
			"test positive",
			atlassianv1.CycleSettings{Concurrency: 1},
			true,
			"",
		},
		{
			"test positive large",
			atlassianv1.CycleSettings{Concurrency: 20},
			true,
			"",
		},
		{
			"test 0",
			atlassianv1.CycleSettings{Concurrency: 0},
			false,
			concurrencyEqualsZeroMessage,
		},
		{
			"test negative",
			atlassianv1.CycleSettings{Concurrency: -1},
			false,
			concurrencyLessThanZeroMessage,
		},
		{
			"test negative large",
			atlassianv1.CycleSettings{Concurrency: -20},
			false,
			concurrencyLessThanZeroMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := validateCycleSettings(tt.cycleSettings)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.reason, reason)
		})
	}
}

func TestValidateMetadata(t *testing.T) {
	tests := []struct {
		name   string
		meta   metav1.ObjectMeta
		ok     bool
		reason string
	}{
		{
			"test ok name and labels",
			metav1.ObjectMeta{Name: "test-a", Labels: map[string]string{cnrNameLabelKey: "test"}},
			true,
			"",
		},
		{
			"test ok name and no labels",
			metav1.ObjectMeta{Name: "test-a"},
			true,
			"",
		},
		{
			"test ok generate name and labels",
			metav1.ObjectMeta{GenerateName: "test-a-", Labels: map[string]string{cnrNameLabelKey: "test"}},
			true,
			"",
		},
		{
			"test ok generate name and no labels",
			metav1.ObjectMeta{GenerateName: "test-a-"},
			true,
			"",
		},
		{
			"test not okay name and no labels",
			metav1.ObjectMeta{Name: "test-a-3ABC3"},
			false,
			`name is not valid: a DNS-1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')`,
		},
		{
			"test too long name and no labels",
			metav1.ObjectMeta{Name: strings.Repeat("a", 255)},
			false,
			"name is not valid: must be no more than 253 characters",
		},
		{
			"test ok name and not okay label",
			metav1.ObjectMeta{Name: "test", Labels: map[string]string{cnrNameLabelKey: "DEADBEEF-3735928559"}},
			false,
			`label value is not valid: a DNS-1123 label must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character (e.g. 'my-name',  or '123-abc', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?')`,
		},
		{
			"test ok name and not too long label",
			metav1.ObjectMeta{Name: "test", Labels: map[string]string{cnrNameLabelKey: strings.Repeat("a", 64)}},
			false,
			"label value is not valid: must be no more than 63 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := validateMetadata(tt.meta)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.reason, reason)
		})
	}
}

func TestValidateSelectorWithNodes(t *testing.T) {

	nodes := test.BuildTestNodes(10, test.NodeOpts{
		LabelKey:   "select",
		LabelValue: "me",
	})
	var names []string
	for _, node := range nodes {
		names = append(names, node.Name)
	}

	selector, _ := labels.Parse("select=other")
	nodes = append(nodes, test.BuildTestNode(test.NodeOpts{
		Name:       "other",
		LabelKey:   "select",
		LabelValue: "other",
	}))
	names = append(names, "other")

	tests := []struct {
		name     string
		nodes    []*v1.Node
		selector labels.Selector
		match    []string
		ok       bool
		reason   string
	}{
		{
			"basic-test",
			test.BuildTestNodes(10, test.NodeOpts{}),
			labels.Everything(),
			nil,
			true,
			"",
		},
		{
			"no nodes",
			nil,
			labels.Everything(),
			nil,
			false,
			"node group is scaled to 0",
		},
		{
			"matches nodes",
			nodes,
			labels.Everything(),
			names,
			true,
			"",
		},
		{
			"select nodes",
			nodes,
			selector,
			nil,
			true,
			"",
		},
		{
			"select nodes and match",
			nodes,
			selector,
			[]string{"other"},
			true,
			"",
		},
		{
			"select nodes and match not exist",
			nodes,
			selector,
			[]string{"other-missing"},
			false,
			`the node "other-missing" does not exist in the nodegroup but it is specified to cycle`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeLister := test.NewTestNodeWatcher(tt.nodes, test.NodeListerOptions{ReturnErrorOnList: false})
			ok, reason := validateSelectorWithNodes(nodeLister, tt.selector, tt.match)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.reason, reason)
		})
	}
}
