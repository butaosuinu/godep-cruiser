package engine

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
	"github.com/butaosuinu/godep-cruiser/internal/testcorpus"
)

func TestViolationCorpus(t *testing.T) {
	t.Parallel()

	cases, err := testcorpus.Load(filepath.Join("..", "..", "testdata", "corpus"))
	if err != nil {
		t.Fatalf("testcorpus.Load() error = %v", err)
	}
	configurations := corpusConfigurations()
	if len(configurations) != len(cases) {
		t.Fatalf("corpus configuration count = %d, want %d", len(configurations), len(cases))
	}

	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()

			configuration, ok := configurations[fixture.ID]
			if !ok {
				t.Fatalf("no engine configuration for corpus case %q", fixture.ID)
			}
			resolver, err := scanner.NewResolverFromGoMod(filepath.Join(fixture.ModuleDir, "go.mod"))
			if err != nil {
				t.Fatalf("scanner.NewResolverFromGoMod() error = %v", err)
			}
			files, err := scanner.Scan(fixture.ModuleDir, resolver)
			if err != nil {
				t.Fatalf("scanner.Scan() error = %v", err)
			}
			violations, err := Evaluate(&configuration, files)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}

			got := projectCorpusViolations(violations)
			if !reflect.DeepEqual(got, fixture.Violations) {
				t.Errorf("Evaluate() violations =\n%#v\nwant golden =\n%#v", got, fixture.Violations)
			}
		})
	}
}

func corpusConfigurations() map[string]config.Config {
	orphan := true
	lessThanTwo := 2
	moreThanTwo := 2

	return map[string]config.Config{
		"baseline-expiry": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "core-no-adapters",
				Severity: config.SeverityWarn,
				From:     config.From{Path: []string{`^internal/core/`}},
				To: config.To{
					Path:            []string{`^internal/adapters/`},
					DependencyTypes: []config.DependencyType{config.DependencyTypeLocal},
				},
			}},
		},
		"forbidden-import-target": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "entrypoint-not-importable",
				Severity: config.SeverityError,
				From:     config.From{Path: []string{`^internal/`}},
				To: config.To{
					Path:            []string{`^cmd/`},
					DependencyTypes: []config.DependencyType{config.DependencyTypeLocal},
				},
			}},
		},
		"layer-direction": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "layer-direction",
				Severity: config.SeverityError,
				From: config.From{
					Path:    []string{`^internal/core/`},
					PathNot: []string{`^internal/core/migration\.go$`},
				},
				To: config.To{
					Path:            []string{`^internal/infra(?:/|$)`},
					DependencyTypes: []config.DependencyType{config.DependencyTypeLocal},
				},
			}},
		},
		"number-of-dependents": {
			Forbidden: []config.ForbiddenRule{
				{
					Name:     "minimum-two-dependents",
					Severity: config.SeverityError,
					From: config.From{
						Path:                       []string{`^internal/(?:hub|leaf)/`},
						NumberOfDependentsLessThan: &lessThanTwo,
					},
					To: config.To{},
				},
				{
					Name:     "maximum-two-dependents",
					Severity: config.SeverityError,
					From: config.From{
						Path:                       []string{`^internal/hub/`},
						NumberOfDependentsMoreThan: &moreThanTwo,
					},
					To: config.To{},
				},
			},
		},
		"orphan-file": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "no-orphans",
				Severity: config.SeverityError,
				From:     config.From{Orphan: &orphan},
				To:       config.To{},
			}},
		},
		"package-main-placement": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "package-main-placement",
				Severity: config.SeverityError,
				From: config.From{
					PathNot:     []string{`^cmd/`, `^tools/`},
					PackageName: []string{`^main$`},
				},
				To: config.To{},
			}},
		},
		"required-dependency": {
			Required: []config.RequiredRule{{
				Name:     "handler-requires-logging",
				Severity: config.SeverityError,
				From:     config.From{Path: []string{`^internal/handler/`}},
				To: config.To{
					Path:            []string{`^internal/logging$`},
					DependencyTypes: []config.DependencyType{config.DependencyTypeLocal},
				},
			}},
		},
		"stdlib-denylist-exception": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "core-stdlib-denylist",
				Severity: config.SeverityError,
				From: config.From{Path: []string{
					`^internal/core/agent/agent\.go()$`,
					`^internal/core/(.+)$`,
				}},
				To: config.To{
					Path:            []string{`^(?:net|os)$`},
					PathNot:         []string{`^$1os$`},
					DependencyTypes: []config.DependencyType{config.DependencyTypeStdlib},
				},
			}},
		},
		"stdlib-only-tree": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "tools-stdlib-only",
				Severity: config.SeverityError,
				From:     config.From{Path: []string{`^tools/`}},
				To: config.To{
					DependencyTypesNot: []config.DependencyType{config.DependencyTypeStdlib},
				},
			}},
		},
		"third-party-in-core": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "core-no-third-party",
				Severity: config.SeverityError,
				From:     config.From{Path: []string{`^internal/core/`}},
				To: config.To{
					DependencyTypes: []config.DependencyType{config.DependencyTypeModule},
				},
			}},
		},
		"unclassified-dependency": {
			Allowed: []config.AllowedRule{{
				Name: "dependencies-must-be-classified",
				From: config.From{Path: []string{`^internal/app/`}},
				To: config.To{
					Path:            []string{`^internal/core$`},
					DependencyTypes: []config.DependencyType{config.DependencyTypeLocal},
				},
			}},
			AllowedSeverity: config.SeverityError,
		},
	}
}

func projectCorpusViolations(violations []Violation) []testcorpus.ExpectedViolation {
	projected := make([]testcorpus.ExpectedViolation, 0, len(violations))
	for _, violation := range violations {
		expected := testcorpus.ExpectedViolation{
			Rule:     violation.Rule,
			Severity: string(violation.Severity),
			From: testcorpus.Location{
				Path: violation.From.Path,
				Line: violation.From.Line,
			},
		}
		if violation.To != nil {
			expected.To = &testcorpus.Dependency{
				Path:           violation.To.Path,
				DependencyType: string(violation.To.Type),
			}
		}
		projected = append(projected, expected)
	}

	return projected
}
