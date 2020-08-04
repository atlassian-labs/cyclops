package command

import (
	"github.com/spf13/cobra"
)

// GetFlagString gets a string value from a command with a flag name or panics
func GetFlagString(cmd *cobra.Command, flag string) string {
	v, err := cmd.Flags().GetString(flag)
	if err != nil {
		panic(err)
	}
	return v
}
