package k8s

import (
	"github.com/spf13/cobra"

	"github.com/atlassian-labs/cyclops/pkg/cli/kubeplug/command"
)

// NamespaceFlag gets the --namespace flag from the command
func NamespaceFlag(cmd *cobra.Command) string {
	return command.GetFlagString(cmd, "namespace")
}
