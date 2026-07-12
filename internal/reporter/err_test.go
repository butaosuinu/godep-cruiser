package reporter

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
)

func TestSummarize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		violations []engine.Violation
		want       Summary
	}{
		{
			name: "empty input",
		},
		{
			name: "counts every supported severity",
			violations: []engine.Violation{
				{Severity: "error"},
				{Severity: "warn"},
				{Severity: "info"},
				{Severity: "ignore"},
			},
			want: Summary{Total: 4, Error: 1, Warn: 1, Info: 1, Ignore: 1},
		},
		{
			name: "unknown severity contributes only to total",
			violations: []engine.Violation{
				{Severity: "error"},
				{Severity: "unexpected"},
			},
			want: Summary{Total: 2, Error: 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := Summarize(test.violations); got != test.want {
				t.Errorf("Summarize() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestSummarizeReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report Report
		want   Summary
	}{
		{
			name: "empty report",
		},
		{
			name: "stale entries are errors",
			report: Report{
				Stale: []baseline.StaleError{
					{Entry: baseline.Entry{Rule: "removed", From: "old.go"}},
					{Entry: baseline.Entry{Rule: "also-removed", From: "older.go"}},
				},
			},
			want: Summary{Total: 2, Error: 2},
		},
		{
			name: "stale entries are added to violation counts",
			report: Report{
				Violations: []engine.Violation{
					{Severity: "error"},
					{Severity: "warn"},
					{Severity: "info"},
					{Severity: "ignore"},
				},
				Stale: []baseline.StaleError{{
					Entry: baseline.Entry{Rule: "removed", From: "old.go"},
				}},
			},
			want: Summary{Total: 5, Error: 2, Warn: 1, Info: 1, Ignore: 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := SummarizeReport(test.report); got != test.want {
				t.Errorf("SummarizeReport() = %#v, want %#v", got, test.want)
			}
			if got := ReportErrorCount(test.report); got != test.want.Error {
				t.Errorf("ReportErrorCount() = %d, want %d", got, test.want.Error)
			}
		})
	}
}

func TestWriteErrGolden(t *testing.T) {
	t.Parallel()

	violations := []engine.Violation{
		{
			Rule:     "core-purity",
			Comment:  "  move the import\n\tbehind an interface  ",
			Severity: "error",
			Kind:     engine.ViolationKindForbidden,
			From:     engine.Source{Path: "internal/core/service.go", Line: 12},
			To: &engine.Dependency{
				Path: "database/sql",
				Type: "stdlib",
			},
		},
		{
			Rule:     engine.NotInAllowedRuleName,
			Severity: "warn",
			Kind:     engine.ViolationKindNotAllowed,
			From:     engine.Source{Path: "internal/app/app.go", Line: 8},
			To: &engine.Dependency{
				Path: "internal/misc",
				Type: "local",
			},
		},
		{
			Rule:     "main-placement",
			Comment:  "move package main under cmd/ or tools/",
			Severity: "error",
			Kind:     engine.ViolationKindForbidden,
			From:     engine.Source{Path: "internal/worker/main.go", Line: 1},
		},
	}

	want, err := os.ReadFile("testdata/err.golden")
	if err != nil {
		t.Fatalf("ReadFile(err.golden) error = %v", err)
	}

	var got bytes.Buffer
	if err := WriteErr(&got, violations); err != nil {
		t.Fatalf("WriteErr() error = %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("WriteErr() = %q, want %q", got.Bytes(), want)
	}
	if got := ErrorCount(violations); got != 2 {
		t.Errorf("ErrorCount() = %d, want 2", got)
	}
}

func TestWriteErrSpecialCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		violations []engine.Violation
		want       string
	}{
		{
			name: "empty input has no success footer",
		},
		{
			name: "unknown edge kind is explicit",
			violations: []engine.Violation{{
				Rule:     "custom",
				Severity: "info",
				Kind:     "custom-kind",
				From:     engine.Source{Path: "custom.go", Line: 3},
				To:       &engine.Dependency{Path: "example.com/custom", Type: "module"},
			}},
			want: "[info] rule \"custom\": custom.go:3 -> example.com/custom (module): unknown violation kind \"custom-kind\"\n",
		},
		{
			name: "unknown source kind is explicit and blank comment is omitted",
			violations: []engine.Violation{{
				Rule:     "custom-source",
				Comment:  " \n\t ",
				Severity: "ignore",
				Kind:     "custom-kind",
				From:     engine.Source{Path: "custom.go", Line: 5},
			}},
			want: "[ignore] rule \"custom-source\": custom.go:5: unknown violation kind \"custom-kind\"\n",
		},
		{
			name: "diagnostic values cannot add lines",
			violations: []engine.Violation{{
				Rule:     "escaped",
				Severity: "error",
				Kind:     engine.ViolationKindForbidden,
				From:     engine.Source{Path: "source\nfile.go", Line: 2},
				To:       &engine.Dependency{Path: "target\rpath", Type: "local"},
			}},
			want: "[error] rule \"escaped\": source\\nfile.go:2 -> target\\rpath (local): forbidden dependency\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var got bytes.Buffer
			if err := WriteErr(&got, test.violations); err != nil {
				t.Fatalf("WriteErr() error = %v", err)
			}
			if got.String() != test.want {
				t.Errorf("WriteErr() = %q, want %q", got.String(), test.want)
			}
		})
	}
}

func TestWriteErrReportStaleBaselineEntries(t *testing.T) {
	t.Parallel()

	edgeTarget := "example.com/removed"
	tests := []struct {
		name   string
		report Report
		want   string
	}{
		{
			name: "source-only stale entry",
			report: Report{Stale: []baseline.StaleError{{
				Entry: baseline.Entry{Rule: "old-source-rule", From: "old.go"},
			}}},
			want: "[error] baseline entry is stale: rule \"old-source-rule\", from \"old.go\"; remove this entry from the baseline.\n",
		},
		{
			name: "violation precedes edge stale entry",
			report: Report{
				Violations: []engine.Violation{{
					Rule:     "current",
					Severity: "warn",
					Kind:     engine.ViolationKindForbidden,
					From:     engine.Source{Path: "current.go", Line: 4},
				}},
				Stale: []baseline.StaleError{{
					Entry: baseline.Entry{
						Rule: "old-edge-rule",
						From: "old.go",
						To:   &edgeTarget,
					},
				}},
			},
			want: "[warn] rule \"current\": current.go:4: forbidden source\n" +
				"[error] baseline entry is stale: rule \"old-edge-rule\", from \"old.go\", to \"example.com/removed\"; remove this entry from the baseline.\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var got bytes.Buffer
			if err := WriteErrReport(&got, test.report); err != nil {
				t.Fatalf("WriteErrReport() error = %v", err)
			}
			if got.String() != test.want {
				t.Errorf("WriteErrReport() = %q, want %q", got.String(), test.want)
			}
		})
	}
}

func TestWriteErrPropagatesWriterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	tests := []struct {
		name  string
		write func() error
	}{
		{
			name: "violation",
			write: func() error {
				return WriteErr(errFailingWriter{err: wantErr}, []engine.Violation{{
					Rule:     "rule",
					Severity: "error",
					Kind:     engine.ViolationKindForbidden,
					From:     engine.Source{Path: "source.go", Line: 1},
				}})
			},
		},
		{
			name: "stale baseline entry",
			write: func() error {
				return WriteErrReport(errFailingWriter{err: wantErr}, Report{
					Stale: []baseline.StaleError{{
						Entry: baseline.Entry{Rule: "removed", From: "old.go"},
					}},
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := test.write(); !errors.Is(err, wantErr) {
				t.Errorf("write error = %v, want %v", err, wantErr)
			}
		})
	}
}

type errFailingWriter struct {
	err error
}

func (writer errFailingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
