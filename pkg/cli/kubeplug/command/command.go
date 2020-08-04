package command

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// FlagFlagger wraps a method of adding a flag to a FlagSet
type FlagFlagger interface {
	AddFlags(*pflag.FlagSet)
}

// CmdFlagger wraps a method of adding a flag to a Command
type CmdFlagger interface {
	AddFlags(*cobra.Command)
}

// Describer describes a CLI application
type Describer interface {
	Usage() string
	Version() string
	Example() string
}

// RunOrDie runs the cobra command or panics
func RunOrDie(usage, version, example string, run func(*cobra.Command, []string), ff []FlagFlagger, cf []CmdFlagger) {
	cmd := &cobra.Command{
		Use:     usage,
		Version: version,
		Example: example,

		Run: func(cmd *cobra.Command, args []string) {
			run(cmd, args)
		},
	}

	flags := cmd.PersistentFlags()

	for _, flagger := range ff {
		flagger.AddFlags(flags)
	}

	for _, flagger := range cf {
		flagger.AddFlags(cmd)
	}

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
