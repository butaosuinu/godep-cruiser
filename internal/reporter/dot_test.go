package reporter

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

func TestWriteDOTReport(t *testing.T) {
	t.Parallel()

	dependency := &engine.Dependency{Path: `internal\dep"quoted`, Type: "module"}
	report := Report{
		Violations: []engine.Violation{
			{
				Rule:     `deny "quoted"\rule`,
				Severity: "error",
				From:     engine.Source{Path: `internal\source"quoted.go`, Line: 7},
				To:       dependency,
			},
			{
				Rule:     "second",
				Severity: "warn",
				From:     engine.Source{Path: `internal\source"quoted.go`, Line: 7},
				To:       dependency,
			},
			{
				Rule:     "no\norphans",
				Severity: "info",
				From:     engine.Source{Path: "lonely.go", Line: 1},
			},
		},
		Stale: []baseline.StaleError{{
			Entry: baseline.Entry{Rule: "old", From: "old.go"},
		}},
	}
	const want = `digraph violations {
  rankdir=LR;
  node [shape=box];
  n0 [label="internal\\source\"quoted.go"];
  n1 [label="internal\\dep\"quoted (module)"];
  n2 [label="lonely.go (no\norphans [info] @ line 1)", color="#cf222e", fillcolor="#ffebe9", penwidth=2, style="filled"];
  stale0 [label="[error] baseline entry is stale: rule \"old\", from \"old.go\"; remove this entry from the baseline.", color="#cf222e", fillcolor="#ffebe9", penwidth=2, style="filled"];
  n0 -> n1 [label="line 7: deny \"quoted\"\\rule [error]; second [warn]", color="#cf222e", penwidth=3];
}
`

	var got bytes.Buffer
	if err := WriteDOTReport(&got, report); err != nil {
		t.Fatalf("WriteDOTReport() error = %v", err)
	}
	if got.String() != want {
		t.Errorf("WriteDOTReport() =\n%s\nwant =\n%s", got.String(), want)
	}
	if strings.Contains(got.String(), "stale0 ->") || strings.Contains(got.String(), "-> stale0") {
		t.Errorf("WriteDOTReport() connected the stale node:\n%s", got.String())
	}
}

func TestWriteDOTEmptyReport(t *testing.T) {
	t.Parallel()

	const want = `digraph violations {
  rankdir=LR;
  node [shape=box];
  no_violations [label="No violations"];
}
`

	var got bytes.Buffer
	if err := WriteDOT(&got, nil); err != nil {
		t.Fatalf("WriteDOT() error = %v", err)
	}
	if got.String() != want {
		t.Errorf("WriteDOT() =\n%s\nwant =\n%s", got.String(), want)
	}
}

func TestEscapeDOTQuotedString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "quote", value: `a"b`, want: `a\"b`},
		{name: "backslash", value: `a\b`, want: `a\\b`},
		{name: "line breaks", value: "a\nb\rc", want: `a\nb\rc`},
		{name: "UTF-8", value: "日本語", want: "日本語"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := escapeDOTQuotedString(test.value); got != test.want {
				t.Errorf("escapeDOTQuotedString(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestWriteDOTPropagatesWriterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	err := WriteDOT(dotErrorWriter{err: wantErr}, nil)
	if !errors.Is(err, wantErr) {
		t.Errorf("WriteDOT() error = %v, want it to wrap %v", err, wantErr)
	}
}

type dotErrorWriter struct {
	err error
}

func (writer dotErrorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
