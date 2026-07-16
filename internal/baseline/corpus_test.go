package baseline_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func TestBaselineCorpus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		fixtureID     string
		configuration config.Config
	}{
		{
			name:      "module-scoped edge",
			fixtureID: "baseline-expiry",
			configuration: config.Config{Forbidden: []config.ForbiddenRule{{
				Name:     "core-no-adapters",
				Severity: config.SeverityWarn,
				From:     config.From{Path: []string{`^internal/core/`}},
				To: config.To{
					Path:            []string{`^internal/adapters/`},
					DependencyTypes: []config.DependencyType{config.DependencyTypeLocal},
				},
			}}},
		},
		{
			name:      "folder-scoped package edge",
			fixtureID: "folder-scope",
			configuration: config.Config{Forbidden: []config.ForbiddenRule{{
				Name:     "app-no-blocked",
				Severity: config.SeverityWarn,
				Scope:    config.ScopeFolder,
				From:     config.From{Path: []string{`^internal/app$`}},
				To:       config.To{Path: []string{`^internal/blocked/`}},
			}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			testBaselineCorpus(t, test.fixtureID, test.configuration)
		})
	}
}

func testBaselineCorpus(t *testing.T, fixtureID string, configuration config.Config) {
	t.Helper()

	moduleDir := filepath.Join("..", "..", "testdata", "corpus", fixtureID)
	resolver, err := scanner.NewResolverFromGoMod(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		t.Fatalf("scanner.NewResolverFromGoMod() error = %v", err)
	}
	files, err := scanner.Scan(moduleDir, resolver)
	if err != nil {
		t.Fatalf("scanner.Scan() error = %v", err)
	}
	violations, err := engine.Evaluate(&configuration, files)
	if err != nil {
		t.Fatalf("engine.Evaluate() error = %v", err)
	}

	baselineFile, err := os.Open(filepath.Join(moduleDir, "baseline.json"))
	if err != nil {
		t.Fatalf("os.Open(baseline.json) error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := baselineFile.Close(); closeErr != nil {
			t.Errorf("close baseline.json: %v", closeErr)
		}
	})
	known, err := baseline.Load(baselineFile)
	if err != nil {
		t.Fatalf("baseline.Load() error = %v", err)
	}

	got := projectCorpusResult(baseline.Apply(known, violations))
	want := loadCorpusGolden(t, filepath.Join(moduleDir, "baseline.golden.json"))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("baseline corpus result =\n%#v\nwant golden =\n%#v", got, want)
	}
}

type corpusResult struct {
	Violations []corpusFinding `json:"violations"`
	Known      []corpusFinding `json:"known"`
	Stale      []corpusFinding `json:"stale"`
}

type corpusFinding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	From     string `json:"from"`
	To       string `json:"to,omitempty"`
	Message  string `json:"message,omitempty"`
}

func projectCorpusResult(result baseline.Result) corpusResult {
	projected := corpusResult{
		Violations: make([]corpusFinding, 0, len(result.Violations)),
		Known:      make([]corpusFinding, 0, len(result.Known)),
		Stale:      make([]corpusFinding, 0, len(result.Stale)),
	}
	for _, violation := range result.Violations {
		projected.Violations = append(projected.Violations, projectViolation(violation))
	}
	for _, violation := range result.Known {
		projected.Known = append(projected.Known, projectViolation(violation))
	}
	for _, stale := range result.Stale {
		finding := corpusFinding{
			Rule:     stale.Entry.Rule,
			Severity: string(config.SeverityError),
			From:     stale.Entry.From,
			Message:  stale.Error(),
		}
		if stale.Entry.To != nil {
			finding.To = *stale.Entry.To
		}
		projected.Stale = append(projected.Stale, finding)
	}

	return projected
}

func projectViolation(violation engine.Violation) corpusFinding {
	finding := corpusFinding{
		Rule:     violation.Rule,
		Severity: string(violation.Severity),
		From:     violation.From.Path,
	}
	if violation.To != nil {
		finding.To = violation.To.ImportPath
		if finding.To == "" {
			finding.To = violation.To.Path
		}
	}

	return finding
}

func loadCorpusGolden(t *testing.T, filename string) corpusResult {
	t.Helper()

	file, err := os.Open(filename)
	if err != nil {
		t.Fatalf("os.Open(baseline.golden.json) error = %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close baseline.golden.json: %v", closeErr)
		}
	}()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var golden corpusResult
	if err := decoder.Decode(&golden); err != nil {
		t.Fatalf("decode baseline.golden.json: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("decode trailing baseline.golden.json: %v", err)
	}

	return golden
}
