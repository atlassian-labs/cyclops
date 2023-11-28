package slack

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_getSlackChannelID(t *testing.T) {
	tests := []struct {
		name     string
		setEnv   func(t *testing.T)
		expect   string
		expectOk bool
	}{
		{
			name: "test channel ID from env var",
			setEnv: func(t *testing.T) {
				t.Setenv(slackChannelID, "test_channel_id")
			},
			expect:   "test_channel_id",
			expectOk: true,
		},
		{
			name:     "test missing channel ID",
			setEnv:   func(t *testing.T) {},
			expect:   "",
			expectOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variables
			tt.setEnv(t)

			result, ok := getSlackChannelID()
			assert.Equal(t, tt.expect, result)
			assert.Equal(t, tt.expectOk, ok)
		})
	}
}

func Test_getSlackBotToken(t *testing.T) {
	tests := []struct {
		name     string
		setEnv   func(t *testing.T)
		expect   string
		expectOk bool
	}{
		{
			name: "test bot token from file",
			setEnv: func(t *testing.T) {
				tempDir := t.TempDir()
				file, err := os.Create(tempDir + "test_bot_token_file")
				if err != nil {
					fmt.Println(err)
				}
				defer file.Close()

				if _, err = file.WriteString("test_bot_token"); err != nil {
					fmt.Println(err)
				}
				t.Setenv(slackBotUserOAuthAccessTokenFile, file.Name())
			},
			expect:   "test_bot_token",
			expectOk: true,
		},
		{
			name: "test bot token from env var",
			setEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv(slackBotUserOAuthAccessToken, "test_bot_token")
			},
			expect:   "test_bot_token",
			expectOk: true,
		},
		{
			name:     "test missing bot token",
			setEnv:   func(t *testing.T) {},
			expect:   "",
			expectOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variables
			tt.setEnv(t)

			result, ok := getSlackBotToken()
			assert.Equal(t, tt.expect, result)
			assert.Equal(t, tt.expectOk, ok)
		})
	}
}
