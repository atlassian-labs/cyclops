package main

import (
	"github.com/atlassian-labs/cyclops/pkg/cli"
	"github.com/atlassian-labs/cyclops/pkg/cli/kubeplug"
)

var (
	// replaced by ldflags at buildtime
	version = "undefined" //nolint:golint,varcheck,deadcode,unused
)

func main() {
	app := cli.NewCycle(version)
	kubeplug.App(app)
}
