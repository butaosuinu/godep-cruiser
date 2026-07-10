// Package main implements the godep-cruiser command, a deliberately thin CLI
// wrapper around the dependency-rule engine that later issues will add as an
// importable library. It intentionally does no flag parsing beyond --version so
// the library API shape is decided in the implementation issue tree rather than
// pre-empted here.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is the tool version. It is overridden at build time via
// -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point: it takes the CLI arguments and the output
// streams and returns the process exit code. Keeping the real logic here (and
// main() a one-liner) lets tests drive the CLI without spawning a process.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, version)
		return 0
	}

	fmt.Fprintln(stderr, "usage: godep-cruiser --version")
	return 2
}
