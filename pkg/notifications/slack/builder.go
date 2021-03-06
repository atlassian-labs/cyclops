package slack

import (
	"fmt"
	"os"

	"github.com/atlassian-labs/cyclops/pkg/notifications"
	slackapi "github.com/slack-go/slack"
)

// NewNotifier returns a new Slack notifier
func NewNotifier() (notifications.Notifier, error) {
	channelID := os.Getenv("SLACK_CHANNEL_ID")
	token := os.Getenv("SLACK_BOT_USER_OAUTH_ACCESS_TOKEN")

	// Return an error is no slack oauth token is provided
	if token == "" {
		return nil, fmt.Errorf("missing slack oauth token")
	}

	// Return an error if no slack channel is specified
	if channelID == "" {
		return nil, fmt.Errorf("missing slack channel id")
	}

	n := &notifier{
		client:    slackapi.New(token),
		channelID: channelID,
	}

	// Check that the slack app has been added to the channel in the workspace
	_, err := n.client.GetConversationInfo(channelID, false)
	if err != nil {
		return nil, err
	}

	return n, nil
}
