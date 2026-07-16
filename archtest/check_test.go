package archtest_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/archtest"
	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/cruiser"
)

func TestCheckReportsThroughTestingTB(t *testing.T) {
	t.Parallel()

	fixtureRoot := filepath.Join("..", "testdata", "corpus", "forbidden-import-target")
	baseOptions := cruiser.Options{ScanRoot: fixtureRoot}
	target := "example.com/godep-cruiser-fixtures/forbidden-import-target/cmd/shared"
	staleOptions := baseOptions
	staleOptions.Baseline = &cruiser.Baseline{Entries: []cruiser.BaselineEntry{
		{
			Rule: "entrypoint-not-importable",
			From: "internal/app/app.go",
			To:   &target,
		},
		{
			Rule: "removed-rule",
			From: "internal/removed/removed.go",
		},
	}}

	tests := []struct {
		name          string
		configuration *config.Config
		options       cruiser.Options
		wantFatal     int
		wantError     int
		wantLog       int
		wantMessages  []string
	}{
		{
			name:          "error violations fail the test in one report",
			configuration: multipleErrorConfiguration(),
			options:       baseOptions,
			wantError:     1,
			wantMessages: []string{
				`[error] rule "entrypoint-not-importable"`,
				`[error] rule "entrypoint-still-not-importable"`,
			},
		},
		{
			name:          "warning violation is logged",
			configuration: forbiddenConfiguration(config.SeverityWarn),
			options:       baseOptions,
			wantLog:       1,
			wantMessages:  []string{`[warn] rule "entrypoint-not-importable"`},
		},
		{
			name:          "informational violation is logged",
			configuration: forbiddenConfiguration(config.SeverityInfo),
			options:       baseOptions,
			wantLog:       1,
			wantMessages:  []string{`[info] rule "entrypoint-not-importable"`},
		},
		{
			name:          "stale baseline entry fails the test",
			configuration: forbiddenConfiguration(config.SeverityError),
			options:       staleOptions,
			wantError:     1,
			wantMessages:  []string{`baseline entry is stale: rule "removed-rule"`},
		},
		{
			name:          "configuration error stops the test",
			configuration: nil,
			options:       baseOptions,
			wantFatal:     1,
			wantMessages:  []string{"configuration is nil"},
		},
		{
			name:          "scan setup error stops the test",
			configuration: forbiddenConfiguration(config.SeverityError),
			options: cruiser.Options{
				ScanRoot: filepath.Join(fixtureRoot, "missing"),
			},
			wantFatal:    1,
			wantMessages: []string{"initialize resolver"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			recorder := &recordingTB{TB: t}
			archtest.Check(recorder, test.configuration, test.options)

			if recorder.helperCalls != 1 {
				t.Errorf("Helper() calls = %d, want 1", recorder.helperCalls)
			}
			if got := len(recorder.fatalMessages); got != test.wantFatal {
				t.Errorf("Fatalf() calls = %d, want %d", got, test.wantFatal)
			}
			if got := len(recorder.errorMessages); got != test.wantError {
				t.Errorf("Errorf() calls = %d, want %d", got, test.wantError)
			}
			if got := len(recorder.logMessages); got != test.wantLog {
				t.Errorf("Logf() calls = %d, want %d", got, test.wantLog)
			}
			for _, want := range test.wantMessages {
				if got := recorder.message(); !strings.Contains(got, want) {
					t.Errorf("recorded message = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func forbiddenConfiguration(severity config.Severity) *config.Config {
	return &config.Config{Forbidden: []config.ForbiddenRule{
		forbiddenRule("entrypoint-not-importable", severity),
	}}
}

func multipleErrorConfiguration() *config.Config {
	return &config.Config{Forbidden: []config.ForbiddenRule{
		forbiddenRule("entrypoint-not-importable", config.SeverityError),
		forbiddenRule("entrypoint-still-not-importable", config.SeverityError),
	}}
}

func forbiddenRule(name string, severity config.Severity) config.ForbiddenRule {
	return config.ForbiddenRule{
		Name:     name,
		Severity: severity,
		From:     config.From{Path: []string{`^internal/`}},
		To: config.To{
			Path:            []string{`^cmd/`},
			DependencyTypes: []config.DependencyType{config.DependencyTypeLocal},
		},
	}
}

type recordingTB struct {
	testing.TB

	helperCalls   int
	fatalMessages []string
	errorMessages []string
	logMessages   []string
}

func (tb *recordingTB) Helper() {
	tb.helperCalls++
}

func (tb *recordingTB) Fatalf(format string, args ...any) {
	tb.fatalMessages = append(tb.fatalMessages, fmt.Sprintf(format, args...))
}

func (tb *recordingTB) Errorf(format string, args ...any) {
	tb.errorMessages = append(tb.errorMessages, fmt.Sprintf(format, args...))
}

func (tb *recordingTB) Logf(format string, args ...any) {
	tb.logMessages = append(tb.logMessages, fmt.Sprintf(format, args...))
}

func (tb *recordingTB) message() string {
	messages := make([]string, 0, len(tb.fatalMessages)+len(tb.errorMessages)+len(tb.logMessages))
	messages = append(messages, tb.fatalMessages...)
	messages = append(messages, tb.errorMessages...)
	messages = append(messages, tb.logMessages...)

	return strings.Join(messages, "\n")
}
