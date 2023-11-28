# Slack

- [Slack](#slack)
  - [Installation](#installation)
  - [Evironment Variables](#environment-variables)
  - [Common issues, caveats and gotchas](#common-issues-caveats-and-gotchas)


Currently the only messaging provider Cyclops supports is Slack. Notifications are an optional feature and this can enabled when required.

## Installation

The Cyclops notification feature is built as a backend for a Slack integration. We do not distribute a Slack app, this means that to use the notification feature, you will need to [create](https://api.slack.com/apps) your own app in Slack.

Once you have created your app, you will need to add the `chat:write` scope under `Bot Token Scopes` in the `OAuth & Permissions` section. You will also need to add the [scopes](https://api.slack.com/methods/conversations.info) required to check if the bot is in the channel on startup.

You can now install the app to your workspace.

## Evironment Variables

Cyclops will need the following information to successfully push notifications to Slack:

1. Slack Bot Token is the auth token which can be obtained from the `Basic Information` page of your app in Slack. Make sure to copy `Bot User OAuth Access Token`. You can then provide this token to Cyclops via one of the following methods: 
   1. (Recommended) Mount the token as a file from a secret in you Kubernetes cluster, then set the `SLACK_BOT_USER_OAUTH_ACCESS_TOKEN_FILE` environment variable to the path of this file.
   2. Alternatively, you can directly set the `SLACK_BOT_USER_OAUTH_ACCESS_TOKEN` environment variable to the token.

2. Environment Variable `SLACK_CHANNEL_ID` is id of the channel in which you want to post your notifications. Navigate to your Slack workspace on the browser `https://<workspace>.slack.com`. Create the channel in which you want to add Cyclops. The url should be in the form `https://app.slack.com/client/<workspace_id>/<channel_id>`. Copy the channel id from the url.

3. Environment Variable `CLUSTER_NAME` is the name of your cluster which you will need to pass in.

## Common issues, caveats and gotchas

- Ensure that you have added the Slack app to the channel before posting any notifications or else nothing will appear.
