package reporter

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

func TestWriteJSONRoundTrip(t *testing.T) {
	t.Parallel()

	edgeDependency := &engine.Dependency{
		Path:       "internal/core",
		ImportPath: "example.com/project/internal/core",
		Type:       "local",
	}
	tests := []struct {
		name       string
		violations []engine.Violation
		want       JSONReport
		golden     string
	}{
		{
			name: "edge and source-only violations preserve all metadata and order",
			violations: []engine.Violation{
				{
					Rule:     "no-core-import",
					Comment:  "move through <adapter> & \"gateway\"",
					Severity: "error",
					Kind:     engine.ViolationKindForbidden,
					From: engine.Source{
						Path:        "internal/app/app.go",
						Line:        12,
						PackageName: "app",
					},
					To: edgeDependency,
				},
				{
					Rule:     "main-placement",
					Comment:  "move package main under cmd",
					Severity: "warn",
					Kind:     engine.ViolationKindForbidden,
					From: engine.Source{
						Path:        "internal/worker/main.go",
						Line:        7,
						PackageName: "main",
					},
				},
			},
			want: JSONReport{
				SchemaVersion: JSONSchemaVersion,
				Violations: []JSONViolation{
					{
						Rule:     "no-core-import",
						Comment:  "move through <adapter> & \"gateway\"",
						Severity: "error",
						Kind:     "forbidden",
						From: JSONSource{
							Path:        "internal/app/app.go",
							Line:        12,
							PackageName: "app",
						},
						To: &JSONDependency{
							Path:           "internal/core",
							ImportPath:     "example.com/project/internal/core",
							DependencyType: "local",
						},
					},
					{
						Rule:     "main-placement",
						Comment:  "move package main under cmd",
						Severity: "warn",
						Kind:     "forbidden",
						From: JSONSource{
							Path:        "internal/worker/main.go",
							Line:        7,
							PackageName: "main",
						},
					},
				},
				Summary: Summary{Total: 2, Error: 1, Warn: 1},
			},
			golden: "testdata/json.golden",
		},
		{
			name:       "empty input remains an empty array",
			violations: nil,
			want: JSONReport{
				SchemaVersion: JSONSchemaVersion,
				Violations:    []JSONViolation{},
				Summary:       Summary{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			if err := WriteJSON(&output, test.violations); err != nil {
				t.Fatalf("WriteJSON() error = %v", err)
			}
			if !strings.HasSuffix(output.String(), "\n") {
				t.Errorf("WriteJSON() output has no final newline: %q", output.String())
			}
			if strings.Contains(output.String(), `\u003c`) || strings.Contains(output.String(), `\u0026`) {
				t.Errorf("WriteJSON() escaped HTML characters: %q", output.String())
			}
			if test.golden != "" {
				want, err := os.ReadFile(test.golden)
				if err != nil {
					t.Fatalf("ReadFile(%s) error = %v", test.golden, err)
				}
				if !bytes.Equal(output.Bytes(), want) {
					t.Errorf("WriteJSON() =\n%s\nwant golden =\n%s", output.Bytes(), want)
				}
			}

			var got JSONReport
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got.SchemaVersion != 1 {
				t.Errorf("schemaVersion = %d, want 1", got.SchemaVersion)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("decoded JSONReport =\n%#v\nwant =\n%#v", got, test.want)
			}

			var roundTrip bytes.Buffer
			encoder := json.NewEncoder(&roundTrip)
			encoder.SetEscapeHTML(false)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(got); err != nil {
				t.Fatalf("round-trip Encode() error = %v", err)
			}
			if roundTrip.String() != output.String() {
				t.Errorf("round-trip output =\n%s\nwant stable output =\n%s", roundTrip.String(), output.String())
			}
		})
	}
}

func TestWriteJSONReportStaleBaselineEntries(t *testing.T) {
	t.Parallel()

	edgeTarget := "example.com/removed"
	tests := []struct {
		name        string
		stale       baseline.StaleError
		want        JSONStaleBaselineEntry
		wantToField bool
	}{
		{
			name:  "source-only entry omits to",
			stale: baseline.StaleError{Entry: baseline.Entry{Rule: "old-source", From: "old.go"}},
			want: JSONStaleBaselineEntry{
				Rule:    "old-source",
				From:    "old.go",
				Message: "baseline entry is stale: rule \"old-source\", from \"old.go\"; remove this entry from the baseline.",
			},
		},
		{
			name: "edge entry includes to",
			stale: baseline.StaleError{Entry: baseline.Entry{
				Rule: "old-edge",
				From: "old.go",
				To:   &edgeTarget,
			}},
			want: JSONStaleBaselineEntry{
				Rule:    "old-edge",
				From:    "old.go",
				To:      &edgeTarget,
				Message: "baseline entry is stale: rule \"old-edge\", from \"old.go\", to \"example.com/removed\"; remove this entry from the baseline.",
			},
			wantToField: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			if err := WriteJSONReport(&output, Report{
				Stale: []baseline.StaleError{test.stale},
			}); err != nil {
				t.Fatalf("WriteJSONReport() error = %v", err)
			}

			var got JSONReport
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if len(got.StaleBaselineEntries) != 1 {
				t.Fatalf("len(StaleBaselineEntries) = %d, want 1", len(got.StaleBaselineEntries))
			}
			if !reflect.DeepEqual(got.StaleBaselineEntries[0], test.want) {
				t.Errorf("StaleBaselineEntries[0] = %#v, want %#v", got.StaleBaselineEntries[0], test.want)
			}
			if got.Summary != (Summary{Total: 1, Error: 1}) {
				t.Errorf("Summary = %#v, want total=1 error=1", got.Summary)
			}

			entryJSON, err := json.Marshal(got.StaleBaselineEntries[0])
			if err != nil {
				t.Fatalf("json.Marshal(stale entry) error = %v", err)
			}
			if hasToField := strings.Contains(string(entryJSON), `"to"`); hasToField != test.wantToField {
				t.Errorf("stale entry JSON has to field = %t, want %t: %s", hasToField, test.wantToField, entryJSON)
			}
		})
	}
}

func TestWriteJSONPropagatesWriterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	err := WriteJSON(jsonErrorWriter{err: wantErr}, nil)
	if !errors.Is(err, wantErr) {
		t.Errorf("WriteJSON() error = %v, want it to wrap %v", err, wantErr)
	}
}

type jsonErrorWriter struct {
	err error
}

func (writer jsonErrorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
