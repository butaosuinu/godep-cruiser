package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "version flag prints the version and exits 0",
			args:       []string{"--version"},
			wantCode:   0,
			wantStdout: version + "\n",
		},
		{
			name:       "no arguments prints usage and exits 2",
			args:       nil,
			wantCode:   2,
			wantStderr: "usage:",
		},
		{
			name:       "unknown arguments print usage and exit 2",
			args:       []string{"scan", "./..."},
			wantCode:   2,
			wantStderr: "usage:",
		},
		{
			name:       "version flag with extra arguments is not treated as --version",
			args:       []string{"--version", "extra"},
			wantCode:   2,
			wantStderr: "usage:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			got := run(tt.args, &stdout, &stderr)

			if got != tt.wantCode {
				t.Errorf("run(%q) exit code = %d, want %d", tt.args, got, tt.wantCode)
			}
			if tt.wantStdout != "" && stdout.String() != tt.wantStdout {
				t.Errorf("run(%q) stdout = %q, want %q", tt.args, stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("run(%q) stderr = %q, want it to contain %q", tt.args, stderr.String(), tt.wantStderr)
			}
		})
	}
}
