package slack

import (
	"fmt"
	"os"
	"strings"

	"github.com/atlassian-labs/cyclops/pkg/notifications"
	slackapi "github.com/slack-go/slack"
)

const (
	slackBotUserOAuthAccessTokenFile = "SLACK_BOT_USER_OAUTH_ACCESS_TOKEN_FILE"
	slackBotUserOAuthAccessToken     = "SLACK_BOT_USER_OAUTH_ACCESS_TOKEN"
	slackChannelID                   = "SLACK_CHANNEL_ID"
)

// NewNotifier returns a new Slack notifier
func NewNotifier() (notifications.Notifier, error) {
	// Return an error is no slack oauth token is provided
	token, ok := getSlackBotToken()
	if !ok {
		return nil, fmt.Errorf("missing slack oauth token")
	}

	// Return an error if no Slack channel is specified
	channelID, ok := getSlackChannelID()
	if !ok {
		return nil, fmt.Errorf("missing slack channel id")
	}

	n := &notifier{
		client:    slackapi.New(token),
		channelID: channelID,
	}

	// Check that the Slack app has been added to the channel in the workspace
	if _, err := n.client.GetConversationInfo(channelID, false); err != nil {
		return nil, err
	}

	return n, nil
}

func getSlackChannelID() (string, bool) {
	// Read channel ID from env var
	channelID, ok := os.LookupEnv(slackChannelID)
	return channelID, ok
}

func getSlackBotToken() (string, bool) {
	// Check if Slack token is provided as a file, file name is provided in env var
	tokenFile, ok := os.LookupEnv(slackBotUserOAuthAccessTokenFile)

	// If env var for token file is not set, read token from env var directly
	if !ok {
		token, ok := os.LookupEnv(slackBotUserOAuthAccessToken)
		return token, ok
	}

	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(token)), true
}
