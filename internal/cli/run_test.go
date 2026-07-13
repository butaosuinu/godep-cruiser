package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
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

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
