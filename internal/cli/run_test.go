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

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
