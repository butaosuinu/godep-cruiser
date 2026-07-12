package reporter

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

func TestWriteMermaidGolden(t *testing.T) {
	t.Parallel()

	dependency := &engine.Dependency{
		Path:       "x/end[\"dep\"]#&<日本>|line\\break\ntwo",
		ImportPath: "example.com/raw",
		Type:       "module",
	}
	tests := []struct {
		name       string
		violations []engine.Violation
		golden     string
	}{
		{
			name: "violating edges and source-only nodes are highlighted safely",
			violations: []engine.Violation{
				{
					Rule:     "core \"purity\"",
					Severity: "error",
					Kind:     engine.ViolationKindForbidden,
					From:     engine.Source{Path: "o/end[core]#&.go", Line: 12},
					To:       dependency,
				},
				{
					Rule:     "allowed#fallback",
					Severity: "warn",
					Kind:     engine.ViolationKindNotAllowed,
					From:     engine.Source{Path: "o/end[core]#&.go", Line: 12},
					To:       dependency,
				},
				{
					Rule:     "no\norphans",
					Severity: "info",
					Kind:     engine.ViolationKindForbidden,
					From:     engine.Source{Path: "internal/end.go", Line: 1},
				},
			},
			golden: "testdata/mermaid.golden",
		},
		{
			name:   "empty input remains renderable",
			golden: "testdata/mermaid-empty.golden",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			want, err := os.ReadFile(test.golden)
			if err != nil {
				t.Fatalf("ReadFile(%s) error = %v", test.golden, err)
			}

			var got bytes.Buffer
			if err := WriteMermaid(&got, test.violations); err != nil {
				t.Fatalf("WriteMermaid() error = %v", err)
			}
			if !bytes.Equal(got.Bytes(), want) {
				t.Errorf("WriteMermaid() =\n%s\nwant golden =\n%s", got.Bytes(), want)
			}
			if !strings.HasPrefix(got.String(), "flowchart LR\n") {
				t.Errorf("WriteMermaid() does not start with a flowchart declaration: %q", got.String())
			}
			if strings.Contains(got.String(), "```") {
				t.Errorf("WriteMermaid() contains a Markdown fence: %q", got.String())
			}
		})
	}
}

func TestWriteMermaidAggregatesDuplicateEdges(t *testing.T) {
	t.Parallel()

	dependency := &engine.Dependency{Path: "internal/core", Type: "local"}
	violations := []engine.Violation{
		{
			Rule:     "first",
			Severity: "error",
			From:     engine.Source{Path: "internal/app/app.go", Line: 3},
			To:       dependency,
		},
		{
			Rule:     "second",
			Severity: "warn",
			From:     engine.Source{Path: "internal/app/app.go", Line: 3},
			To:       dependency,
		},
	}

	var got bytes.Buffer
	if err := WriteMermaid(&got, violations); err != nil {
		t.Fatalf("WriteMermaid() error = %v", err)
	}
	if edgeCount := strings.Count(got.String(), " -->|"); edgeCount != 1 {
		t.Errorf("WriteMermaid() edge count = %d, want 1:\n%s", edgeCount, got.String())
	}
	if !strings.Contains(got.String(), "first #91;error#93;; second #91;warn#93;") {
		t.Errorf("WriteMermaid() lost aggregated rule labels:\n%s", got.String())
	}
}

func TestWriteMermaidPropagatesWriterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	err := WriteMermaid(mermaidErrorWriter{err: wantErr}, nil)
	if !errors.Is(err, wantErr) {
		t.Errorf("WriteMermaid() error = %v, want it to wrap %v", err, wantErr)
	}
}

type mermaidErrorWriter struct {
	err error
}

func (writer mermaidErrorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
