package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/cruiser"
)

const usage = `usage: godep-cruiser --config FILE [options]

Validate dependency rules by default. Options:
  --config FILE          JSON rule configuration (required)
  --scan-root DIR        Go source root (default ".")
  --baseline FILE        exact-match violation baseline
  --output-type TYPE     err, json, or mermaid (default "err")
  --generate-baseline    write a baseline to stdout and exit 0
  --version              print the version
`

const maxValidationExitCode = 255

// Run executes the CLI with explicit streams and returns the requested process
// exit code. Runtime and usage failures return 2; validation returns its number
// of unsuppressed error violations plus stale baseline entries, capped at 255.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 1 && args[0] == "--version" {
		if _, err := fmt.Fprintln(stdout, version); err != nil {
			return runtimeError(stderr, err)
		}

		return 0
	}

	flags := flag.NewFlagSet("godep-cruiser", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, usage)
	}

	var (
		configPath   string
		scanRoot     string
		baselinePath string
		outputValue  string
		generate     bool
		versionFlag  bool
	)
	flags.StringVar(&configPath, "config", "", "JSON rule configuration")
	flags.StringVar(&scanRoot, "scan-root", ".", "Go source root")
	flags.StringVar(&baselinePath, "baseline", "", "exact-match violation baseline")
	flags.StringVar(&outputValue, "output-type", string(cruiser.OutputTypeErr), "err, json, or mermaid")
	flags.BoolVar(&generate, "generate-baseline", false, "write a baseline to stdout")
	flags.BoolVar(&versionFlag, "version", false, "print the version")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		return 2
	}
	if versionFlag {
		return usageError(stderr, "--version cannot be combined with other arguments")
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "positional arguments are not supported")
	}
	if configPath == "" {
		return usageError(stderr, "--config is required")
	}
	if generate && baselinePath != "" {
		return usageError(stderr, "--generate-baseline cannot be combined with --baseline")
	}

	outputType, ok := outputType(outputValue)
	if !ok {
		return usageError(stderr, fmt.Sprintf("unsupported --output-type %q", outputValue))
	}
	if generate && outputType != cruiser.OutputTypeErr {
		return usageError(stderr, "--generate-baseline cannot be combined with a non-err --output-type")
	}

	configuration, err := config.LoadFile(configPath)
	if err != nil {
		return runtimeError(stderr, err)
	}

	options := cruiser.Options{
		ScanRoot:  scanRoot,
		GoModPath: filepath.Join(scanRoot, "go.mod"),
	}
	if baselinePath != "" {
		known, loadErr := cruiser.LoadBaselineFile(baselinePath)
		if loadErr != nil {
			return runtimeError(stderr, loadErr)
		}
		options.Baseline = &known
	}

	result, err := cruiser.Validate(configuration, options)
	if err != nil {
		return runtimeError(stderr, err)
	}
	if generate {
		if err := cruiser.WriteBaseline(stdout, cruiser.GenerateBaseline(result.Violations)); err != nil {
			return runtimeError(stderr, err)
		}

		return 0
	}
	if err := cruiser.WriteReport(stdout, outputType, result); err != nil {
		return runtimeError(stderr, err)
	}

	return validationExitCode(result.ErrorCount())
}

func validationExitCode(errorCount int) int {
	return min(errorCount, maxValidationExitCode)
}

func outputType(value string) (cruiser.OutputType, bool) {
	switch cruiser.OutputType(value) {
	case cruiser.OutputTypeErr, cruiser.OutputTypeJSON, cruiser.OutputTypeMermaid:
		return cruiser.OutputType(value), true
	default:
		return "", false
	}
}

func usageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "godep-cruiser: %s\n\n%s", message, usage)

	return 2
}

func runtimeError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "godep-cruiser: %v\n", err)

	return 2
}
