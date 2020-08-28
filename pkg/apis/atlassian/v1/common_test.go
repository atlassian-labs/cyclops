package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildNodeGroupNames(t *testing.T) {
	tests := []struct {
		name           string
		nodeGroupsList []string
		nodeGroupName  string
		expect         []string
	}{
		{
			"both nodeGroupsList and nodeGroupName",
			[]string{
				"GroupA",
				"GroupB",
				"GroupC",
			},
			"GroupD",
			[]string{
				"GroupA",
				"GroupB",
				"GroupC",
				"GroupD",
			},
		},
		{
			"nodeGroupsList defined and nodeGroupName empty",
			[]string{
				"GroupA",
				"GroupB",
				"GroupC",
			},
			"",
			[]string{
				"GroupA",
				"GroupB",
				"GroupC",
			},
		},
		{
			"nodeGroupsList empty and nodeGroupName defined",
			[]string{},
			"GroupA",
			[]string{
				"GroupA",
			},
		},
		{
			"nodeGroupsList empty and nodeGroupName defined",
			[]string{},
			"GroupA",
			[]string{
				"GroupA",
			},
		},
		{
			"both nodeGroupsList and nodeGroupName defined with duplication",
			[]string{
				"GroupA",
				"GroupB",
				"GroupC",
			},
			"GroupA",
			[]string{
				"GroupA",
				"GroupB",
				"GroupC",
			},
		},
		{
			"both nodeGroupsList and nodeGroupName empty",
			[]string{},
			"",
			[]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nodeGroupNames := buildNodeGroupNames(test.nodeGroupsList, test.nodeGroupName)
			assert.ElementsMatch(t, nodeGroupNames, test.expect)
		})
	}
}
