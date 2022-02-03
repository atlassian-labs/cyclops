package slack

import (
	"fmt"
	"os"

	"github.com/atlassian-labs/cyclops/pkg/notifications"
	slackapi "github.com/slack-go/slack"
)

// NewNotifier returns a new Slack notifier
func NewNotifier() (notifications.Notifier, error) {
	// Return an error is no slack oauth token is provided
	token, ok := os.LookupEnv("SLACK_BOT_USER_OAUTH_ACCESS_TOKEN")
	if !ok {
		return nil, fmt.Errorf("missing slack oauth token")
	}

	// Return an error if no slack channel is specified
	channelID, ok := os.LookupEnv("SLACK_CHANNEL_ID")
	if !ok {
		return nil, fmt.Errorf("missing slack channel id")
	}

	n := &notifier{
		client:    slackapi.New(token),
		channelID: channelID,
	}

	// Check that the slack app has been added to the channel in the workspace
	if _, err := n.client.GetConversationInfo(channelID, false); err != nil {
		return nil, err
	}

	return n, nil
}
