package reporter

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/internal/baseline"
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

func TestWriteMermaidReportStaleBaselineEntries(t *testing.T) {
	t.Parallel()

	edgeTarget := "example.com/removed"
	tests := []struct {
		name   string
		report Report
		want   []string
	}{
		{
			name: "stale-only report has red standalone nodes",
			report: Report{Stale: []baseline.StaleError{
				{Entry: baseline.Entry{Rule: "old-source", From: "old.go"}},
				{Entry: baseline.Entry{Rule: "old-edge", From: "old.go", To: &edgeTarget}},
			}},
			want: []string{
				`stale0["#91;error#93; baseline entry is stale: rule #34;old-source#34;, from #34;old.go#34;; remove this entry from the baseline."]`,
				`stale1["#91;error#93; baseline entry is stale: rule #34;old-edge#34;, from #34;old.go#34;, to #34;example.com/removed#34;; remove this entry from the baseline."]`,
				"classDef staleBaselineError fill:#ffebe9,stroke:#cf222e,stroke-width:2px",
				"class stale0 staleBaselineError",
				"class stale1 staleBaselineError",
			},
		},
		{
			name: "stale node is added to a violation graph",
			report: Report{
				Violations: []engine.Violation{{
					Rule:     "current",
					Severity: "error",
					From:     engine.Source{Path: "current.go", Line: 2},
					To:       &engine.Dependency{Path: "internal/current", Type: "local"},
				}},
				Stale: []baseline.StaleError{{
					Entry: baseline.Entry{Rule: "old-source", From: "old.go"},
				}},
			},
			want: []string{
				`n0["current.go"]`,
				`n1["internal/current (local)"]`,
				`stale0["#91;error#93; baseline entry is stale: rule #34;old-source#34;, from #34;old.go#34;; remove this entry from the baseline."]`,
				`n0 -->|"line 2: current #91;error#93;"| n1`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var got bytes.Buffer
			if err := WriteMermaidReport(&got, test.report); err != nil {
				t.Fatalf("WriteMermaidReport() error = %v", err)
			}
			if strings.Contains(got.String(), "No violations") {
				t.Errorf("WriteMermaidReport() rendered No violations with stale entries:\n%s", got.String())
			}
			for _, want := range test.want {
				if !strings.Contains(got.String(), want) {
					t.Errorf("WriteMermaidReport() does not contain %q:\n%s", want, got.String())
				}
			}
			for index := range test.report.Stale {
				nodeID := "stale" + strconv.Itoa(index)
				if strings.Contains(got.String(), nodeID+" -->") ||
					strings.Contains(got.String(), "--> "+nodeID) {
					t.Errorf("stale node %d is connected by an edge:\n%s", index, got.String())
				}
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
