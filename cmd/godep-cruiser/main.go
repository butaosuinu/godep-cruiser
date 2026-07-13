// Package main provides the cmd-path compatibility entry point for the
// godep-cruiser command. The module-root entry point supports the documented
// go install path; both wrappers use the same library-backed runner.
package main

import (
	"io"
	"os"

	"github.com/butaosuinu/godep-cruiser/internal/cli"
)

// version is the tool version. It is overridden at build time via
// -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return cli.Run(args, stdout, stderr, version)
}
