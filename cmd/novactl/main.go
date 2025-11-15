// Package main provides the novactl CLI tool for managing NovaEdge resources.
package main

import (
	"os"

	"github.com/piwi3910/novaedge/cmd/novactl/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
