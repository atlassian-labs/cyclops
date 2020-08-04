package main

import (
	"github.com/atlassian-labs/cyclops/pkg/cli"
	"github.com/atlassian-labs/cyclops/pkg/cli/kubeplug"
)

var (
	version = "undefined" // replaced by ldflags at buildtime
)

func main() {
	app := cli.NewCycle(version)
	kubeplug.App(app)
}
