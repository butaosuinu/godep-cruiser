package baseline_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

func TestLoadRejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		document string
		wantText string
	}{
		{name: "unknown top-level field", document: `{"entries":[],"extra":true}`, wantText: `unknown field "extra"`},
		{name: "unknown entry field", document: `{"entries":[{"rule":"r","from":"f.go","extra":true}]}`, wantText: `unknown field "extra"`},
		{name: "duplicate top-level key", document: `{"entries":[],"entries":[]}`, wantText: `duplicate object key "entries"`},
		{name: "duplicate entry key", document: `{"entries":[{"rule":"r","rule":"r","from":"f.go"}]}`, wantText: `duplicate object key "rule"`},
		{name: "empty rule", document: `{"entries":[{"rule":"","from":"f.go"}]}`, wantText: "rule must not be empty"},
		{name: "empty from", document: `{"entries":[{"rule":"r","from":""}]}`, wantText: "from must not be empty"},
		{name: "empty to", document: `{"entries":[{"rule":"r","from":"f.go","to":""}]}`, wantText: "to must not be empty"},
		{name: "null to", document: `{"entries":[{"rule":"r","from":"f.go","to":null}]}`, wantText: "to must be a string when present"},
		{name: "duplicate edge identity", document: `{"entries":[{"rule":"r","from":"f.go","to":"example.com/lib"},{"rule":"r","from":"f.go","to":"example.com/lib"}]}`, wantText: "duplicates entry 0"},
		{name: "duplicate source identity", document: `{"entries":[{"rule":"r","from":"f.go"},{"rule":"r","from":"f.go"}]}`, wantText: "duplicates entry 0"},
		{name: "trailing JSON", document: `{"entries":[]} {"entries":[]}`, wantText: "trailing JSON value"},
		{name: "missing entries", document: `{}`, wantText: "entries is required"},
		{name: "null entries", document: `{"entries":null}`, wantText: "entries must be an array"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := baseline.Load(strings.NewReader(test.document))
			if err == nil {
				t.Fatal("Load() error = nil, want rejection")
			}
			if !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("Load() error = %q, want text %q", err, test.wantText)
			}
		})
	}
}

func TestLoadDoesNotCheckFilesystemExistence(t *testing.T) {
	t.Parallel()

	document := `{"entries":[{"rule":"removed-rule","from":"removed/directory/file.go","to":"example.invalid/removed"}]}`
	got, err := baseline.Load(strings.NewReader(document))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := baseline.Baseline{Entries: []baseline.Entry{{
		Rule: "removed-rule",
		From: "removed/directory/file.go",
		To:   stringPointer("example.invalid/removed"),
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestWriteCanonicalJSONWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	input := baseline.Baseline{Entries: []baseline.Entry{
		{Rule: "z-rule", From: "z.go", To: stringPointer("example.com/z")},
		{Rule: "a-rule", From: "a.go"},
	}}
	original := baseline.Baseline{Entries: append([]baseline.Entry(nil), input.Entries...)}

	var output bytes.Buffer
	if err := baseline.Write(&output, input); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	want := "{\n" +
		"  \"entries\": [\n" +
		"    {\n" +
		"      \"rule\": \"a-rule\",\n" +
		"      \"from\": \"a.go\"\n" +
		"    },\n" +
		"    {\n" +
		"      \"rule\": \"z-rule\",\n" +
		"      \"from\": \"z.go\",\n" +
		"      \"to\": \"example.com/z\"\n" +
		"    }\n" +
		"  ]\n" +
		"}\n"
	if got := output.String(); got != want {
		t.Fatalf("Write() = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("Write() mutated input: got %#v, want %#v", input, original)
	}
}

func TestWriteRejectsInvalidBaseline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseline baseline.Baseline
		wantText string
	}{
		{name: "empty rule", baseline: baseline.Baseline{Entries: []baseline.Entry{{From: "f.go"}}}, wantText: "rule must not be empty"},
		{name: "empty from", baseline: baseline.Baseline{Entries: []baseline.Entry{{Rule: "r"}}}, wantText: "from must not be empty"},
		{name: "empty to", baseline: baseline.Baseline{Entries: []baseline.Entry{{Rule: "r", From: "f.go", To: stringPointer("")}}}, wantText: "to must not be empty"},
		{name: "duplicate identity", baseline: baseline.Baseline{Entries: []baseline.Entry{{Rule: "r", From: "f.go"}, {Rule: "r", From: "f.go"}}}, wantText: "duplicates entry 0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			err := baseline.Write(&output, test.baseline)
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("Write() error = %v, want text %q", err, test.wantText)
			}
		})
	}
}

func TestGenerateWriteLoadApplyRoundTrip(t *testing.T) {
	t.Parallel()

	current := []engine.Violation{
		violation("edge-rule", "edge.go", "example.com/lib", config.SeverityInfo),
		sourceViolation("source-rule", "orphan.go"),
	}
	generated := baseline.Generate(current)

	var document bytes.Buffer
	if err := baseline.Write(&document, generated); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	loaded, err := baseline.Load(bytes.NewReader(document.Bytes()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, generated) {
		t.Fatalf("Load(Write(Generate())) = %#v, want %#v", loaded, generated)
	}

	result := baseline.Apply(loaded, current)
	if len(result.Violations) != 0 || len(result.Stale) != 0 {
		t.Fatalf("round-trip result = %#v, want zero violations and zero stale entries", result)
	}
	if !reflect.DeepEqual(result.Known, current) {
		t.Fatalf("round-trip Known = %#v, want %#v", result.Known, current)
	}
}
