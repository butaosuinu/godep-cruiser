package reporter

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

func TestWriteHTMLReport(t *testing.T) {
	t.Parallel()

	staleTarget := "example.com/<removed>&target"
	tests := []struct {
		name       string
		report     Report
		want       []string
		wantAbsent []string
		checkOrder bool
	}{
		{
			name: "violations and stale entries preserve metadata and escape HTML",
			report: Report{
				Violations: []engine.Violation{
					{
						Rule:     `deny <script> & "edge"`,
						Comment:  `move through <adapter> & "gateway"`,
						Severity: "error",
						Kind:     engine.ViolationKindForbidden,
						From:     engine.Source{Path: "internal/<core>&service.go", Line: 12},
						To: &engine.Dependency{
							Path: "internal/<infra>&store",
							Type: "local",
						},
					},
					{
						Rule:     "handler-requires-logging",
						Severity: "warn",
						Kind:     engine.ViolationKindRequired,
						From:     engine.Source{Path: "internal/handler/missing.go", Line: 1},
					},
				},
				Stale: []baseline.StaleError{
					{Entry: baseline.Entry{Rule: "old-source", From: "old<&>.go"}},
					{Entry: baseline.Entry{Rule: "old-edge", From: "old.go", To: &staleTarget}},
				},
			},
			want: []string{
				"<dt>Total</dt><dd>4</dd>",
				"<dt>Error</dt><dd>3</dd>",
				"<dt>Warn</dt><dd>1</dd>",
				`deny &lt;script&gt; &amp; &#34;edge&#34;`,
				`internal/&lt;core&gt;&amp;service.go:12`,
				`internal/&lt;infra&gt;&amp;store</code> (local)`,
				`move through &lt;adapter&gt; &amp; &#34;gateway&#34;`,
				"handler-requires-logging",
				"internal/handler/missing.go:1",
				"source-only",
				"Stale baseline entries",
				"old&lt;&amp;&gt;.go",
				"example.com/&lt;removed&gt;&amp;target",
			},
			wantAbsent: []string{
				`deny <script>`,
				`internal/<core>&service.go`,
				`internal/<infra>&store`,
				`move through <adapter>`,
				"<nil>",
			},
			checkOrder: true,
		},
		{
			name: "folder violation omits nonexistent line zero",
			report: Report{Violations: []engine.Violation{{
				Rule:     "package-boundary",
				Severity: "error",
				Kind:     engine.ViolationKindForbidden,
				From:     engine.Source{Path: "internal/app"},
				To:       &engine.Dependency{Path: "internal/core", Type: "local"},
			}}},
			want: []string{
				"<td><code>internal/app</code></td>",
				"<code>internal/core</code> (local)",
			},
			wantAbsent: []string{"internal/app:0"},
		},
		{
			name:   "empty report remains a complete page",
			report: Report{},
			want: []string{
				"<!doctype html>",
				"<dt>Total</dt><dd>0</dd>",
				"<dt>Error</dt><dd>0</dd>",
				"<dt>Warn</dt><dd>0</dd>",
				"<dt>Info</dt><dd>0</dd>",
				"<dt>Ignore</dt><dd>0</dd>",
				"No violations.",
				"No stale baseline entries.",
				"</html>\n",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			if err := WriteHTMLReport(&output, test.report); err != nil {
				t.Fatalf("WriteHTMLReport() error = %v", err)
			}
			for _, want := range test.want {
				if !strings.Contains(output.String(), want) {
					t.Errorf("WriteHTMLReport() does not contain %q:\n%s", want, output.String())
				}
			}
			for _, unwanted := range test.wantAbsent {
				if strings.Contains(output.String(), unwanted) {
					t.Errorf("WriteHTMLReport() contains unescaped %q:\n%s", unwanted, output.String())
				}
			}
			if test.checkOrder {
				first := strings.Index(output.String(), "deny &lt;script&gt;")
				second := strings.Index(output.String(), "handler-requires-logging")
				if first == -1 || second == -1 || first >= second {
					t.Errorf("WriteHTMLReport() did not preserve violation order:\n%s", output.String())
				}
			}
		})
	}
}

func TestWriteHTMLIsSelfContained(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := WriteHTML(&output, nil); err != nil {
		t.Fatalf("WriteHTML() error = %v", err)
	}

	if got := strings.Count(output.String(), "<style>"); got != 1 {
		t.Errorf("inline style count = %d, want 1", got)
	}
	for _, external := range []string{"<script", "<link", "href=", "src=", "@import", "url("} {
		if strings.Contains(strings.ToLower(output.String()), external) {
			t.Errorf("WriteHTML() contains script or external asset marker %q", external)
		}
	}
}

func TestWriteHTMLPropagatesWriterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	err := WriteHTML(htmlErrorWriter{err: wantErr}, nil)
	if !errors.Is(err, wantErr) {
		t.Errorf("WriteHTML() error = %v, want it to wrap %v", err, wantErr)
	}
}

type htmlErrorWriter struct {
	err error
}

func (writer htmlErrorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
