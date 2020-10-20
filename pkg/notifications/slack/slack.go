package slack

import (
	"fmt"
	"strings"
	"time"

	v1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	slackapi "github.com/slack-go/slack"
)

type notifier struct {
	client    *slackapi.Client
	channelID string
}

var (
	// ProviderName is the name of the messaging provider
	ProviderName = "slack"

	// Text formatting of the notification message, required in slack
	markdownType = "mrkdwn"

	// Color of the attachment bar in the Slack status notification
	blueColor  = "#3a72f4"
	greenColor = "#1dd32c"
	redColor   = "#e52023"

	// Length of delay required to allow the reply message to enter the thread
	timeDelay = 500 * time.Millisecond
)

// Returns any newly selected nodes to prevent duplicate notifying
func newSelectedNodeNames(cnr *v1.CycleNodeRequest) []string {
	newSelectedNodesNames := []string{}

	for _, node := range cnr.Status.CurrentNodes {
		if _, ok := cnr.Status.SelectedNodes[node.Name]; !ok {
			newSelectedNodesNames = append(newSelectedNodesNames, node.Name)
		}

		cnr.Status.SelectedNodes[node.Name] = true
	}

	return newSelectedNodesNames
}

// Generates the structure for the cycle status notification
func (n *notifier) generateThreadMessage(cnr *v1.CycleNodeRequest) slackapi.Attachment {
	var statusColor, progressText string

	// Determines the colour of the LHS status bar
	switch cnr.Status.Phase {
	case v1.CycleNodeRequestSuccessful:
		statusColor = greenColor
	case v1.CycleNodeRequestFailed:
		statusColor = redColor
	default:
		statusColor = blueColor
	}

	// Wait until all nodes to terminate have been added to the cnr before displaying
	// This is useful when no node names are specified and all nodes in the cnr as to be cycled
	if len(cnr.Status.NodesToTerminate) > 0 {
		progressText = fmt.Sprintf("%d/%d (%d%%)", cnr.Status.NumNodesCycled, len(cnr.Status.NodesToTerminate), int(float64(cnr.Status.NumNodesCycled)/float64(len(cnr.Status.NodesToTerminate))*100))
	}

	nodeGroupTitle := "Nodegroup"
	nodeGroupList := cnr.Spec.NodeGroupsList

	if cnr.Spec.NodeGroupName != "" {
		nodeGroupList = append([]string{cnr.Spec.NodeGroupName}, nodeGroupList...)
	}

	// If there are multiple nodegroups, adjust the title
	if (cnr.Spec.NodeGroupName != "" && len(cnr.Spec.NodeGroupsList) > 0) || len(cnr.Spec.NodeGroupsList) > 1 {
		nodeGroupTitle = "Nodegroups"
	}

	return slackapi.Attachment{
		Color: statusColor,
		Blocks: slackapi.Blocks{
			BlockSet: []slackapi.Block{
				slackapi.NewSectionBlock(nil, []*slackapi.TextBlockObject{
					slackapi.NewTextBlockObject(markdownType, fmt.Sprintf("*Name:*\n%s", cnr.Name), false, false),
					slackapi.NewTextBlockObject(markdownType, fmt.Sprintf("*Progress:*\n%s", progressText), false, false),
					slackapi.NewTextBlockObject(markdownType, fmt.Sprintf("*Cluster:*\n%s", cnr.ClusterName), false, false),
					slackapi.NewTextBlockObject(markdownType, fmt.Sprintf("*Method:*\n%s", cnr.Spec.CycleSettings.Method), false, false),
					slackapi.NewTextBlockObject(markdownType, fmt.Sprintf("*%s:*\n%s", nodeGroupTitle, strings.Join(nodeGroupList, "\n")), false, false),
					slackapi.NewTextBlockObject(markdownType, fmt.Sprintf("*Concurrency:*\n%d", cnr.Spec.CycleSettings.Concurrency), false, false),
				}, nil),
			},
		},
	}
}

// CyclingStarted pushes the main status notification when cycle has started
func (n *notifier) CyclingStarted(cnr *v1.CycleNodeRequest) error {
	_, timestamp, err := n.client.PostMessage(n.channelID, slackapi.MsgOptionAttachments(n.generateThreadMessage(cnr)))
	if err != nil {
		return err
	}

	// Delay required to allow the reply message to enter the thread
	time.Sleep(timeDelay)

	cnr.Status.ThreadTimestamp = timestamp
	return nil
}

// PhaseTransitioned pushes a threaded notification when the cycling transitions to a new phase
func (n *notifier) PhaseTransitioned(cnr *v1.CycleNodeRequest) error {
	if cnr.Status.ThreadTimestamp == "" {
		return fmt.Errorf("ThreadTimestamp not set in CycleNodeRequest")
	}

	// If the cycling succeeded, update the cycle status notification
	if cnr.Status.Phase == v1.CycleNodeRequestSuccessful {
		if _, _, _, err := n.client.UpdateMessage(n.channelID, cnr.Status.ThreadTimestamp, slackapi.MsgOptionAttachments(n.generateThreadMessage(cnr))); err != nil {
			return err
		}
	}

	// If the cycling failed, update the cycle status notification and add the error message from the cycleNodeRequest
	if cnr.Status.Phase == v1.CycleNodeRequestFailed {
		message := n.generateThreadMessage(cnr)

		message.Blocks.BlockSet = append(message.Blocks.BlockSet, []slackapi.Block{
			slackapi.NewSectionBlock(slackapi.NewTextBlockObject(markdownType, fmt.Sprintf("```%v```", cnr.Status.Message), false, false), nil, nil),
		}...)

		if _, _, _, err := n.client.UpdateMessage(n.channelID, cnr.Status.ThreadTimestamp, slackapi.MsgOptionAttachments(message)); err != nil {
			return err
		}
	}

	messageParameters := slackapi.NewPostMessageParameters()
	messageParameters.ThreadTimestamp = cnr.Status.ThreadTimestamp

	_, _, err := n.client.PostMessage(n.channelID, slackapi.MsgOptionPostMessageParameters(messageParameters), slackapi.MsgOptionBlocks(slackapi.NewSectionBlock(nil, []*slackapi.TextBlockObject{
		slackapi.NewTextBlockObject(markdownType, fmt.Sprintf("Entered the *%s* phase", cnr.Status.Phase), false, false),
	}, nil)))

	return err
}

// NodesSelected pushes a threaded notification showing which new nodes are being cycled
func (n *notifier) NodesSelected(cnr *v1.CycleNodeRequest) error {
	// Sanity check to make sure not to post an empty update
	selectedNodes := newSelectedNodeNames(cnr)
	if len(selectedNodes) == 0 {
		return fmt.Errorf("No new nodes selected")
	}

	messageParameters := slackapi.NewPostMessageParameters()
	messageParameters.ThreadTimestamp = cnr.Status.ThreadTimestamp

	blocks := []slackapi.Block{
		slackapi.NewSectionBlock(nil, []*slackapi.TextBlockObject{
			slackapi.NewTextBlockObject(markdownType, "Nodes selected for cycling", false, false),
			slackapi.NewTextBlockObject(markdownType, fmt.Sprintf("```%v```", strings.Join(selectedNodes, "\n")), false, false),
		}, nil),
	}

	_, _, err := n.client.PostMessage(n.channelID, slackapi.MsgOptionPostMessageParameters(messageParameters), slackapi.MsgOptionBlocks(blocks...))
	if err != nil {
		return err
	}

	// Update the cycle status notification to reflect the total number of nodes cycled so far
	_, _, _, err = n.client.UpdateMessage(n.channelID, cnr.Status.ThreadTimestamp, slackapi.MsgOptionAttachments(n.generateThreadMessage(cnr)))
	return err
}
