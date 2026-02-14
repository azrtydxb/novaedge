// Package main provides the novactl CLI tool for managing NovaEdge resources.
package main

import (
	"os"

	"github.com/piwi3910/novaedge/cmd/novactl/cmd"
)

// Build-time variables set via ldflags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
