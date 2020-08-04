package kubeplug

import (
	"fmt"
	"io"
	"os"

	"github.com/logrusorgru/aurora"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/atlassian-labs/cyclops/pkg/cli/kubeplug/command"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
)

// Plug is a wrapper around a k8s client, corbra, cli-runtime, and cli based IO that help make fully featured kubectl plugins
// a Plug will be setup and passed to a Plugger interface to use for application based logic
type Plug struct {
	Client client.Client

	// Common flagsets
	ConfigFlags   *genericclioptions.ConfigFlags
	PrintFlags    *genericclioptions.PrintFlags
	ResourceFlags *genericclioptions.ResourceBuilderFlags

	// Current Printer and IO options
	Printer printers.ResourcePrinter
	IO      genericclioptions.IOStreams

	// Color typesetter for the cli output
	CLI aurora.Aurora

	// Commonly used flags values
	Namespace string

	// Arguments remaining passed to the cli
	Args []string
}

// Plugger defines an interface that accepts a setup Plug
type Plugger interface {
	Run(plug *Plug)
}

// Application collects all the interface components needed to start an application as a kubectl plugin
type Application interface {
	Plugger
	command.Describer
	command.CmdFlagger
}

// App is a helper to run an Application with Do by components
func App(app Application) {
	Do(app, app, app)
}

// Do sets up everything for a Describer, Plugger and Flaggers, and parses the cobra command
// The plugger is then run with the setup Plug
// Use this method when you want separate components for part or want more than one flagger
// If your struct implements all interfaces in one place, use App()
func Do(description command.Describer, plugger Plugger, moreFlags ...command.CmdFlagger) {
	plug := &Plug{}

	// Setup standard cli flags
	plug.ConfigFlags = genericclioptions.NewConfigFlags(true)
	plug.PrintFlags = genericclioptions.NewPrintFlags("").WithTypeSetter(scheme.Scheme)

	plug.ResourceFlags = genericclioptions.NewResourceBuilderFlags().
		WithScheme(scheme.Scheme).
		WithLabelSelector(labels.Everything().String())

	runCmd := func(cmd *cobra.Command, args []string) {
		plug.Client = k8s.NewCLIClientOrDie(plug.ConfigFlags)
		plug.Namespace = k8s.NamespaceFlag(cmd)
		plug.Args = args

		printer, err := plug.PrintFlags.ToPrinter()
		if err != nil {
			panic(err)
		}
		plug.Printer = printer

		plug.CLI = aurora.NewAurora(isatty.IsTerminal(os.Stderr.Fd()))

		plug.IO = genericclioptions.IOStreams{
			In:     os.Stdin,
			Out:    os.Stdout,
			ErrOut: os.Stderr,
		}

		plugger.Run(plug)
	}

	command.RunOrDie(
		description.Usage(),
		description.Version(),
		description.Example(),
		runCmd,
		[]command.FlagFlagger{plug.ConfigFlags, plug.ResourceFlags},
		append([]command.CmdFlagger{plug.PrintFlags}, moreFlags...),
	)
}

// ResourceVisitor returns a cli-runtime Visitor for listing resources in a functional way
func (p *Plug) ResourceVisitor(resources ...string) resource.Visitor {
	resourceFinder := p.ResourceFlags.ToBuilder(p.ConfigFlags, resources)
	return resourceFinder.Do()
}

// PrintList wraps printing List type resource objects in various formats based on the output type given
func (p *Plug) PrintList(list runtime.Object, w io.Writer) {
	items, _ := meta.ExtractList(list)
	for _, item := range items {
		_ = p.Printer.PrintObj(item, w)
	}
}

// Output to stdout. Used for non message log lines (outputting yaml etc)
func (p *Plug) Output(arg interface{}) {
	_, _ = fmt.Fprint(p.IO.Out, arg)
}

// Message to outputs stderr. Used for cli output that could also be thought of as logs
func (p *Plug) Message(arg interface{}) {
	_, _ = fmt.Fprint(p.IO.ErrOut, arg)
}

// MessageRed outputs a red colored message
func (p *Plug) MessageRed(arg interface{}) {
	p.Message(p.CLI.Red(arg))
}

// MessageGreen outputs a green colored message
func (p *Plug) MessageGreen(arg interface{}) {
	p.Message(p.CLI.Green(arg))
}

// MessageLn outputs to stderr with a new line appended. Used for cli output that could also be thought of as logs
func (p *Plug) MessageLn(arg interface{}) {
	_, _ = fmt.Fprintln(p.IO.ErrOut, arg)
}

// MessageRedLn outputs a red colored message with a new line appended
func (p *Plug) MessageRedLn(arg interface{}) {
	p.MessageLn(p.CLI.Red(arg))
}

// MessageGreenLn outputs a green colored message with a new line appended
func (p *Plug) MessageGreenLn(arg interface{}) {
	p.MessageLn(p.CLI.Green(arg))
}

// MessageFail outputs MessageLn to stderr and then os.Exit(1)
func (p *Plug) MessageFail(arg interface{}) {
	p.MessageRedLn(arg)
	os.Exit(1)
}

// Decorate runs Message only if attached to a CLI and adds the faint modifier
func (p *Plug) Decorate(arg interface{}) {
	if !isatty.IsTerminal(os.Stderr.Fd()) {
		return
	}
	p.Message(p.CLI.Faint(arg))
}

// DecorateLn runs MessageLn only if attached to a CLI and adds the faint modifier
func (p *Plug) DecorateLn(arg interface{}) {
	if !isatty.IsTerminal(os.Stderr.Fd()) {
		return
	}
	p.MessageLn(p.CLI.Faint(arg))
}
