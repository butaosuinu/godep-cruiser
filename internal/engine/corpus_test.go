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
			assertCorpusViolationShapes(t, fixture.ID, violations)

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
	reachable := true
	unreachable := false
	moreUnstable := true

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
		"folder-scope": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "app-no-blocked",
				Severity: config.SeverityWarn,
				Scope:    config.ScopeFolder,
				From:     config.From{Path: []string{`^internal/app$`}},
				To:       config.To{Path: []string{`^internal/blocked/`}},
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
		"more-unstable": {
			Forbidden: []config.ForbiddenRule{
				{
					Name:     "module-more-unstable",
					Severity: config.SeverityError,
					From:     config.From{Path: []string{`^internal/source/`}},
					To:       config.To{MoreUnstable: &moreUnstable},
				},
				{
					Name:     "folder-more-unstable",
					Severity: config.SeverityError,
					Scope:    config.ScopeFolder,
					From:     config.From{Path: []string{`^internal/source$`}},
					To:       config.To{MoreUnstable: &moreUnstable},
				},
			},
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
		"reachable-test-helper": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "production-no-testutil",
				Severity: config.SeverityError,
				From:     config.From{Path: []string{`^internal/app/`}},
				To: config.To{
					Path:      []string{`^internal/testutil$`},
					Reachable: &reachable,
				},
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
		"unreachable-dead-code": {
			Forbidden: []config.ForbiddenRule{{
				Name:     "entrypoint-reaches-production",
				Severity: config.SeverityError,
				From:     config.From{Path: []string{`^cmd/app/`}},
				To: config.To{
					Path:      []string{`^internal/`},
					Reachable: &unreachable,
				},
			}},
		},
	}
}

func assertCorpusViolationShapes(t *testing.T, fixtureID string, violations []Violation) {
	t.Helper()

	switch fixtureID {
	case "reachable-test-helper":
		for _, violation := range violations {
			if violation.Kind != ViolationKindReachable {
				t.Errorf("reachable corpus violation kind = %q, want %q", violation.Kind, ViolationKindReachable)
			}
			if violation.To == nil {
				t.Error("reachable corpus violation To = nil, want package target")
				continue
			}
			if violation.To.ImportPath != "" {
				t.Errorf("reachable corpus violation ImportPath = %q, want empty", violation.To.ImportPath)
			}
		}
	case "unreachable-dead-code":
		for _, violation := range violations {
			if violation.Kind != ViolationKindUnreachable {
				t.Errorf("unreachable corpus violation kind = %q, want %q", violation.Kind, ViolationKindUnreachable)
			}
			if violation.To != nil {
				t.Errorf("unreachable corpus violation To = %#v, want nil", violation.To)
			}
		}
	case "folder-scope":
		for _, violation := range violations {
			if violation.Kind != ViolationKindForbidden {
				t.Errorf("folder-scope corpus violation kind = %q, want %q", violation.Kind, ViolationKindForbidden)
			}
			if violation.From.Path != "internal/app" || violation.From.Line != 0 || violation.From.PackageName != "" {
				t.Errorf("folder-scope corpus From = %#v, want package path with line zero and empty package name", violation.From)
			}
			if violation.To == nil {
				t.Error("folder-scope corpus violation To = nil, want package target")
				continue
			}
			if violation.To.ImportPath != "" {
				t.Errorf("folder-scope corpus violation ImportPath = %q, want empty", violation.To.ImportPath)
			}
			if violation.To.Type != scanner.DependencyTypeLocal {
				t.Errorf("folder-scope corpus violation dependency type = %q, want %q", violation.To.Type, scanner.DependencyTypeLocal)
			}
		}
	case "more-unstable":
		for _, violation := range violations {
			if violation.Kind != ViolationKindForbidden || violation.To == nil || violation.To.Type != scanner.DependencyTypeLocal {
				t.Errorf("more-unstable corpus violation = %#v, want forbidden local edge", violation)
				continue
			}
			switch violation.Rule {
			case "folder-more-unstable":
				if violation.From.Path != "internal/source" || violation.From.Line != 0 ||
					violation.From.PackageName != "" || violation.To.ImportPath != "" {
					t.Errorf("folder more-unstable violation = %#v, want package edge coordinates", violation)
				}
			case "module-more-unstable":
				if violation.From.Path != "internal/source/source.go" || violation.From.Line != 6 ||
					violation.From.PackageName != "source" ||
					violation.To.ImportPath != "example.test/more-unstable/internal/more" {
					t.Errorf("module more-unstable violation = %#v, want source import coordinates", violation)
				}
			default:
				t.Errorf("more-unstable corpus rule = %q, want module or folder rule", violation.Rule)
			}
		}
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
