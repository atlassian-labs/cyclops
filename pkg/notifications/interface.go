package notifications

import v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"

// Notifier provides an interface to interact with a messaging provider, e.g. Slack, Microsoft Teams
type Notifier interface {
	CyclingStarted(*v1.CycleNodeRequest) error
	PhaseTransitioned(*v1.CycleNodeRequest) error
	NodesSelected(*v1.CycleNodeRequest) error
}
