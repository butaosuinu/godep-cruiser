// Package main provides the module-root godep-cruiser command so
// `go install github.com/butaosuinu/godep-cruiser@latest` installs the CLI.
package main

import (
	"io"
	"os"

	"github.com/butaosuinu/godep-cruiser/internal/cli"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return cli.Run(args, stdout, stderr, version)
}
