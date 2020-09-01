package notifierbuilder

import (
	"fmt"

	"github.com/atlassian-labs/cyclops/pkg/notifications"
	"github.com/atlassian-labs/cyclops/pkg/notifications/slack"
)

type builderFunc func() (notifications.Notifier, error)

// BuildNotifier returns a notifier based on the provided name
func BuildNotifier(name string) (notifications.Notifier, error) {
	buildFuncs := map[string]builderFunc{
		slack.ProviderName: slack.NewNotifier,
	}

	builder, ok := buildFuncs[name]
	if !ok {
		return nil, fmt.Errorf("builder for notifier %v not found", name)
	}

	return builder()
}
