package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/atlassian-labs/cyclops/pkg/apis"
	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/cli/kubeplug"
	"github.com/atlassian-labs/cyclops/pkg/generation"
)

const (
	// separator to use for visually separating cli output
	separator = "«─»"
)

// cycle contains the logic and state to run as a kubectl plugin to cycle nodes
type cycle struct {
	plug    *kubeplug.Plug
	version string

	selectAllFlag                *bool
	dryModeFlag                  *bool
	cnrNameFlag                  *string
	concurrencyOverrideFlag      *int64
	nodesFlag                    *[]string
	cyclingTimeout               *time.Duration
	skipInitialHealthChecksFlag  *bool
	skipPreTerminationChecksFlag *bool
}

// NewCycle returns a new cycle CLI application that implements all the interfaces needed for kubeplug
func NewCycle(version string) kubeplug.Application {
	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintln("Unable to setup Kubernetes CRD schemes", err))
	}

	return &cycle{
		version: version,
	}
}

// Usage returns the cli name and usage template for the help message
func (*cycle) Usage() string {
	return `kubectl-cycle --name "cnr-name" <nodegroup names> or`
}

// Version returns the version of this plugin
func (c *cycle) Version() string {
	return c.version
}

// Example returns the detailed examples to display in the help message
func (*cycle) Example() string {
	return `
# cycle system node group
kubectl cycle --name example-123 system
 
# cycle system and ingress node group
kubectl cycle --name example-123 system ingress
 
# cycle by labels
kubectl cycle --name example-123 -l type=default
 
# cycle all nodegroups
kubectl cycle --name example-123 --all

# test cycle all nodegroups with dry mode
kubectl cycle --name example-123 --all --dry

# output CNRs instead of cycling for all nodegroups
kubectl cycle --name example-123 --all -o yaml

# cycle system node group without the initial health checks
kubectl cycle --name example-123 system --skip-initial-health-checks
`
}

// AddFlags implements adding the extra flags for this kubeplug plugin
func (c *cycle) AddFlags(cmd *cobra.Command) {
	c.selectAllFlag = cmd.PersistentFlags().Bool("all", false, "option to allow cycling of all nodegroups")
	c.dryModeFlag = cmd.PersistentFlags().Bool("dry", false, "option to enable dry mode for applying CNRs")
	c.cnrNameFlag = cmd.PersistentFlags().String("name", "", "option to specify name prefix of generated CNRs")
	c.nodesFlag = cmd.PersistentFlags().StringSlice("nodes", nil, "option to specify which nodes of the nodegroup to cycle. Leave empty for all")
	c.concurrencyOverrideFlag = cmd.PersistentFlags().Int64("concurrency", -1, "option to override concurrency of all CNRs. Set for 0 to skip. -1 or not specified will use values from NodeGroup definition")
	c.cyclingTimeout = cmd.PersistentFlags().Duration("cycling-timeout", 0*time.Second, "option to set timeout for cycling. Default to controller defined timeout")
	c.skipInitialHealthChecksFlag = cmd.PersistentFlags().Bool("skip-initial-health-checks", false, "option to skip the initial set of health checks before cycling.")
	c.skipPreTerminationChecksFlag = cmd.PersistentFlags().Bool("skip-pre-termination-checks", false, "option to skip pre-termination checks during cycling.")
}

// Run function called by cobra with args and client ready
func (c *cycle) Run(plug *kubeplug.Plug) {
	c.plug = plug

	if valid, reason := c.validateListOptions(); !valid {
		c.plug.MessageFail(fmt.Sprint("invalid list options.. Reason: ", reason))
	}

	c.plug.MessageLn("fetching nodegroups...")
	nodeGroupList, err := c.listNodeGroupsWithOptions()
	if err != nil {
		c.plug.MessageFail(fmt.Sprint("failed to get all specified node groups: ", err))
	}

	c.plug.MessageLn("generating + validating CNRs")
	c.plug.DecorateLn(separator)
	cnrList, err := c.generateCNRs(nodeGroupList, c.cnrName(), c.cyclopsNamespace())
	if err != nil {
		c.plug.MessageFail(fmt.Sprint("failed to generate CNRs: ", err))
	}
	c.plug.DecorateLn(separator)

	if len(cnrList.Items) == 0 {
		c.plug.MessageGreenLn("No CNRs generated, goodbye!")
		return
	}

	c.outputOrApply(cnrList)
}

// validateListOptions returns if a valid combination of flags and arguments is supplied
func (c *cycle) validateListOptions() (bool, string) {
	hasLabelSelector := c.labelSelector() != ""
	hasArgs := len(c.plug.Args) > 0

	// if --all then no other args should be given
	if c.selectAll() {
		return !hasLabelSelector && !hasArgs, "cannot use label selector or cnr names with --all specified"
	}

	// if not --all, then make sure we have either labels or by name
	if !hasLabelSelector && !hasArgs {
		return false, "no selection arguments given. use --all to select all nodegroups"
	}

	if hasLabelSelector {
		return !hasArgs, "cannot use both --selector and named arguments at the same time"
	}

	return true, ""
}

// listNodeGroupsWithOptions lists or gets the nodegroups based on the options in the cli
func (c *cycle) listNodeGroupsWithOptions() (*atlassianv1.NodeGroupList, error) {
	// get: by arguments
	if len(c.plug.Args) > 0 {
		nodeGroupList, err := generation.GetNodeGroups(c.plug.Client, c.plug.Args...)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get node groups")
		}
		return nodeGroupList, nil
	}

	// list: by selector - empty selector will get all
	labelSelector, err := labels.Parse(c.labelSelector())
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse list options for node groups")
	}

	listOptions := &client.ListOptions{
		LabelSelector: labelSelector,
	}

	nodeGroupList, err := generation.ListNodeGroups(c.plug.Client, listOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list node groups")
	}

	return nodeGroupList, nil
}

// generateCNRs creates the CNRs from the nodegroups and only returns valid ones based on generation.ValidateCNR
func (c *cycle) generateCNRs(nodeGroups *atlassianv1.NodeGroupList, name, namespace string) (*atlassianv1.CycleNodeRequestList, error) {
	var cnrs []atlassianv1.CycleNodeRequest
	for _, nodeGroup := range nodeGroups.Items {
		cnr := generation.GenerateCNR(nodeGroup, c.nodesOverride(), name, namespace)
		generation.GiveReason(&cnr, "cli")
		generation.GiveClientVersion(&cnr, c.version)

		if name == "" {
			generation.UseGenerateNameCNR(&cnr)
		}

		if newConcurrencyValue, set := c.concurrencyOverride(); set {
			cnr.Spec.CycleSettings.Concurrency = newConcurrencyValue
		}

		if newCyclingTimeoutValue, set := c.cyclingTimeoutOverride(); set {
			cnr.Spec.CycleSettings.CyclingTimeout = newCyclingTimeoutValue
		}

		// If the cli argument is not used, this will not be added to the cnr spec
		// This argument overrides the value given in the nodegroup spec
		cnr.Spec.SkipInitialHealthChecks = c.skipInitialHealthChecks()
		cnr.Spec.SkipPreTerminationChecks = c.skipPreTerminationChecks()

		if ok, reason := generation.ValidateCNR(generation.NewOneShotNodeLister(c.plug.Client), cnr); !ok {
			name, suffix := generation.GetNameExample(cnr.ObjectMeta)
			c.plug.MessageLn(fmt.Sprintf("%s %s%s because %s", c.plug.CLI.Cyan("[skipping]"), c.plug.CLI.Yellow(name), c.plug.CLI.BrightBlue(suffix), reason))
			continue
		}
		cnrs = append(cnrs, cnr)
	}

	if len(cnrs) == len(nodeGroups.Items) {
		c.plug.MessageGreenLn("all nodegroups valid!")
	}

	return &atlassianv1.CycleNodeRequestList{Items: cnrs}, nil
}

// outputOrApply
func (c *cycle) outputOrApply(cnrs *atlassianv1.CycleNodeRequestList) {
	if c.plug.PrintFlags.OutputFlagSpecified() {
		c.plug.MessageLn(c.plug.CLI.Yellow("--output specified. outputting CNRs to stdout instead of applying"))
		c.plug.PrintList(cnrs, c.plug.IO.Out)
		return
	}

	action := "[applying]"
	if c.dryMode() {
		action = "[dry mode]"
	}

	var successCount int
	for _, cnr := range cnrs.Items {
		name, suffix := generation.GetNameExample(cnr.ObjectMeta)
		c.plug.Message(fmt.Sprint(c.plug.CLI.Cyan(action), " "))
		c.plug.Message(c.plug.CLI.Yellow(name))
		c.plug.Message(c.plug.CLI.BrightBlue(suffix))

		if err := generation.ApplyCNR(c.plug.Client, c.dryMode(), cnr); err != nil {
			c.plug.MessageLn("")
			c.plug.MessageRed("[ failed ] ")
			c.plug.MessageLn(fmt.Sprint("to apply ", c.plug.CLI.Yellow(name), c.plug.CLI.BrightBlue(suffix), " because ", err))
			continue
		}

		c.plug.MessageGreenLn(" OK")
		successCount++
	}

	kubectlTip := fmt.Sprintf("kubectl -n %s get cnr -l name=%s", c.cyclopsNamespace(), c.cnrName())

	c.plug.DecorateLn(separator)
	c.plug.MessageGreenLn(fmt.Sprintf("DONE! Applied %d CNRs successfully", successCount))

	if successCount != len(cnrs.Items) {
		c.plug.MessageRedLn(fmt.Sprintf("%d CNRs failed", len(cnrs.Items)-successCount))
	}

	if c.cnrName() != "" {
		c.plug.DecorateLn(separator)
		c.plug.MessageLn(fmt.Sprintf("%s to see applied CNRs", c.plug.CLI.Cyan(kubectlTip)))
	}
}

// selectAll safely returns the --all flag
func (c *cycle) selectAll() bool {
	return c.selectAllFlag != nil && *c.selectAllFlag
}

// dryMode safely returns the --dry flag
func (c *cycle) dryMode() bool {
	return c.dryModeFlag != nil && *c.dryModeFlag
}

// cnrName safely returns the select --name flag
func (c *cycle) cnrName() string {
	if c.cnrNameFlag == nil {
		return ""
	}
	return strings.ToLower(*c.cnrNameFlag)
}

// labelSelector safely returns the --selector flag
func (c *cycle) labelSelector() string {
	if c.plug.ResourceFlags.LabelSelector == nil {
		return ""
	}
	return *c.plug.ResourceFlags.LabelSelector
}

// cyclopsNamespace safely returns the select --namespace flag with default of kube-system
func (c *cycle) cyclopsNamespace() string {
	if c.plug.Namespace == "" {
		return "kube-system"
	}
	return c.plug.Namespace
}

// concurrencyOverride safely returns if the user wants to override the concurrency
func (c *cycle) concurrencyOverride() (int64, bool) {
	if c.concurrencyOverrideFlag == nil || *c.concurrencyOverrideFlag == -1 {
		return 0, false
	}
	return *c.concurrencyOverrideFlag, true
}

// nodesOverride safely returns if the user wants to override the nodes to cycle
func (c *cycle) nodesOverride() []string {
	if c.nodesFlag == nil || len(*c.nodesFlag) == 0 {
		return nil
	}
	return *c.nodesFlag
}

// cyclingTimeoutOverride safely returns if the user wants to override the cycling timeout
func (c *cycle) cyclingTimeoutOverride() (*metav1.Duration, bool) {
	if *c.cyclingTimeout == 0*time.Second {
		return nil, false
	}
	return &metav1.Duration{Duration: *c.cyclingTimeout}, true
}

// skipInitialHealthChecks safely returns the --skip-initial-health-checks flag
func (c *cycle) skipInitialHealthChecks() bool {
	return c.skipInitialHealthChecksFlag != nil && *c.skipInitialHealthChecksFlag
}

// skipInitialHealthChecks safely returns the --skip-pre-termination-checks flag
func (c *cycle) skipPreTerminationChecks() bool {
	return c.skipPreTerminationChecksFlag != nil && *c.skipPreTerminationChecksFlag
}
