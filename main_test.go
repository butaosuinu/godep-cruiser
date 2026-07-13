package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
)

const cliHelperEnvironment = "GO_WANT_GODEP_CRUISER_HELPER"

func TestCLIReportersEndToEnd(t *testing.T) {
	t.Parallel()

	root := repositoryPath(t, "testdata", "corpus", "layer-direction")
	configuration := repositoryPath(t, "testdata", "cli", "layer-direction.json")
	tests := []struct {
		name       string
		outputType string
		golden     string
	}{
		{name: "err", outputType: "err", golden: "layer-direction.err.golden"},
		{name: "json", outputType: "json", golden: "layer-direction.json.golden"},
		{name: "mermaid", outputType: "mermaid", golden: "layer-direction.mermaid.golden"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := executeCLI(t,
				"--config", configuration,
				"--scan-root", root,
				"--output-type", test.outputType,
			)
			if result.exitCode != 1 {
				t.Errorf("exit code = %d, want 1; stderr = %q", result.exitCode, result.stderr)
			}
			if result.stderr != "" {
				t.Errorf("stderr = %q, want empty", result.stderr)
			}
			want := readGolden(t, test.golden)
			if result.stdout != want {
				t.Errorf("stdout =\n%s\nwant golden =\n%s", result.stdout, want)
			}
		})
	}
}

func TestCLIBaselineEndToEnd(t *testing.T) {
	t.Parallel()

	root := repositoryPath(t, "testdata", "corpus", "layer-direction")
	configuration := repositoryPath(t, "testdata", "cli", "layer-direction.json")
	generated := executeCLI(t,
		"--config", configuration,
		"--scan-root", root,
		"--generate-baseline",
	)
	if generated.exitCode != 0 || generated.stderr != "" {
		t.Fatalf("generate exit/stderr = (%d, %q), want (0, empty)", generated.exitCode, generated.stderr)
	}
	if want := readGolden(t, "layer-direction.baseline.golden.json"); generated.stdout != want {
		t.Errorf("generated baseline =\n%s\nwant golden =\n%s", generated.stdout, want)
	}

	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baselinePath, []byte(generated.stdout), 0o600); err != nil {
		t.Fatalf("os.WriteFile(baseline) error = %v", err)
	}
	validated := executeCLI(t,
		"--config", configuration,
		"--scan-root", root,
		"--baseline", baselinePath,
	)
	if validated.exitCode != 0 || validated.stdout != "" || validated.stderr != "" {
		t.Errorf(
			"baseline validation = (exit %d, stdout %q, stderr %q), want all successful and empty",
			validated.exitCode,
			validated.stdout,
			validated.stderr,
		)
	}
}

func TestCLIStaleBaselineEndToEnd(t *testing.T) {
	t.Parallel()

	result := executeCLI(t,
		"--config", repositoryPath(t, "testdata", "cli", "baseline-expiry.json"),
		"--scan-root", repositoryPath(t, "testdata", "corpus", "baseline-expiry"),
		"--baseline", repositoryPath(t, "testdata", "corpus", "baseline-expiry", "baseline.json"),
	)
	if result.exitCode != 1 {
		t.Errorf("exit code = %d, want 1; stderr = %q", result.exitCode, result.stderr)
	}
	if result.stderr != "" {
		t.Errorf("stderr = %q, want empty", result.stderr)
	}
	if want := readGolden(t, "baseline-expiry.err.golden"); result.stdout != want {
		t.Errorf("stdout =\n%s\nwant golden =\n%s", result.stdout, want)
	}
}

func TestCLIExitCodeIsErrorCount(t *testing.T) {
	t.Parallel()

	result := executeCLI(t,
		"--config", repositoryPath(t, "testdata", "cli", "stdlib-denylist-exception.json"),
		"--scan-root", repositoryPath(t, "testdata", "corpus", "stdlib-denylist-exception"),
	)
	if result.exitCode != 2 {
		t.Errorf("exit code = %d, want two error violations; stderr = %q", result.exitCode, result.stderr)
	}
	if got := strings.Count(result.stdout, "[error]"); got != 2 {
		t.Errorf("error diagnostic count = %d, want 2; stdout = %q", got, result.stdout)
	}
	if result.stderr != "" {
		t.Errorf("stderr = %q, want empty", result.stderr)
	}
}

func TestCLIExitCodeCapsLargeErrorCount(t *testing.T) {
	t.Parallel()

	const errorCount = 256
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/exit-code-test\n\ngo 1.25\n")
	writeTestFile(t, filepath.Join(root, "sample.go"), "package sample\n\nimport _ \"example.com/dependency\"\n")

	configuration := config.Config{Forbidden: make([]config.ForbiddenRule, errorCount)}
	for index := range configuration.Forbidden {
		configuration.Forbidden[index] = config.ForbiddenRule{
			Name:     fmt.Sprintf("deny-dependency-%03d", index),
			Severity: config.SeverityError,
			From:     config.From{},
			To:       config.To{Path: []string{`^example\.com/dependency$`}},
		}
	}
	configurationData, err := json.Marshal(configuration)
	if err != nil {
		t.Fatalf("json.Marshal(configuration) error = %v", err)
	}
	configurationPath := filepath.Join(root, "godep-cruiser.json")
	writeTestFile(t, configurationPath, string(configurationData))

	result := executeCLI(t, "--config", configurationPath, "--scan-root", root)
	if result.exitCode != 255 {
		t.Errorf("exit code = %d, want capped status 255; stderr = %q", result.exitCode, result.stderr)
	}
	if got := strings.Count(result.stdout, "[error]"); got != errorCount {
		t.Errorf("error diagnostic count = %d, want %d", got, errorCount)
	}
	if result.stderr != "" {
		t.Errorf("stderr = %q, want empty", result.stderr)
	}
}

func TestREADMEQuickStartEndToEnd(t *testing.T) {
	t.Parallel()

	result := executeCLI(t,
		"--config", repositoryPath(t, "testdata", "cli", "quick-start.json"),
		"--scan-root", repositoryPath(t),
	)
	if result.exitCode != 0 || result.stdout != "" || result.stderr != "" {
		t.Errorf(
			"Quick start = (exit %d, stdout %q, stderr %q), want successful empty report",
			result.exitCode,
			result.stdout,
			result.stderr,
		)
	}
}

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv(cliHelperEnvironment) != "1" {
		return
	}

	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator == -1 {
		os.Exit(125)
	}

	os.Exit(run(os.Args[separator+1:], os.Stdout, os.Stderr))
}

type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func executeCLI(t *testing.T, args ...string) cliResult {
	t.Helper()

	commandArgs := append([]string{"-test.run=^TestCLIHelperProcess$", "--"}, args...)
	command := exec.Command(os.Args[0], commandArgs...)
	command.Env = append(os.Environ(), cliHelperEnvironment+"=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Fatalf("execute CLI: %v", err)
		}
		exitCode = exitError.ExitCode()
	}

	return cliResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

func repositoryPath(t *testing.T, elements ...string) string {
	t.Helper()

	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}

	return filepath.Join(append([]string{root}, elements...)...)
}

func readGolden(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(repositoryPath(t, "testdata", "cli", name))
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", name, err)
	}

	return string(data)
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
