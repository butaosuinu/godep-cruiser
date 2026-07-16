package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/cruiser"
)

func TestRunVersionWriteFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	var stderr bytes.Buffer
	code := Run([]string{"--version"}, failingWriter{err: wantErr}, &stderr, "dev")
	if code != 2 {
		t.Errorf("Run() exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), wantErr.Error()) {
		t.Errorf("Run() stderr = %q, want it to contain %q", stderr.String(), wantErr)
	}
}

func TestRunHelpListsHTML(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--help"}, &stdout, &stderr, "dev")
	if code != 0 {
		t.Errorf("Run(--help) exit code = %d, want 0", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("Run(--help) stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "err, json, mermaid, dot, or html") {
		t.Errorf("Run(--help) stderr does not list HTML output: %q", stderr.String())
	}
}

func TestValidationExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		errorCount int
		want       int
	}{
		{name: "success", errorCount: 0, want: 0},
		{name: "single error", errorCount: 1, want: 1},
		{name: "largest exact count", errorCount: 255, want: 255},
		{name: "first capped count", errorCount: 256, want: 255},
		{name: "multiple of process status range", errorCount: 512, want: 255},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := validationExitCode(test.errorCount); got != test.want {
				t.Errorf("validationExitCode(%d) = %d, want %d", test.errorCount, got, test.want)
			}
		})
	}
}

func TestOutputType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  cruiser.OutputType
		ok    bool
	}{
		{name: "err", value: "err", want: cruiser.OutputTypeErr, ok: true},
		{name: "json", value: "json", want: cruiser.OutputTypeJSON, ok: true},
		{name: "mermaid", value: "mermaid", want: cruiser.OutputTypeMermaid, ok: true},
		{name: "dot", value: "dot", want: cruiser.OutputTypeDOT, ok: true},
		{name: "html", value: "html", want: cruiser.OutputTypeHTML, ok: true},
		{name: "unsupported", value: "yaml"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := outputType(test.value)
			if got != test.want || ok != test.ok {
				t.Errorf("outputType(%q) = (%q, %t), want (%q, %t)", test.value, got, ok, test.want, test.ok)
			}
		})
	}
}

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
