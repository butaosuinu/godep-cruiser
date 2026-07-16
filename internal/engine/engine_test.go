package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/graph"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func TestAllowedFailClosed(t *testing.T) {
	t.Parallel()

	allowCore := config.AllowedRule{
		Name: "allow-core",
		From: config.From{Path: []string{`^internal/app/`}},
		To:   config.To{Path: []string{`^internal/core$`}},
	}
	allowOther := config.AllowedRule{
		Name: "allow-other",
		From: config.From{},
		To:   config.To{Path: []string{`^internal/other$`}},
	}
	allowModule := config.AllowedRule{
		Name: "allow-module",
		From: config.From{},
		To:   config.To{DependencyTypes: []config.DependencyType{config.DependencyTypeModule}},
	}
	file := scanner.File{
		Path:        "internal/app/app.go",
		Package:     "app",
		PackageLine: 1,
		Imports: []scanner.Import{{
			Path:         "example.com/project/internal/core",
			ResolvedPath: "internal/core",
			Type:         scanner.DependencyTypeLocal,
			Line:         3,
		}},
	}

	tests := []struct {
		name          string
		configuration config.Config
		wantCount     int
	}{
		{name: "omitted allowed disables checking", configuration: config.Config{}},
		{
			name: "explicit empty allowed rejects every dependency",
			configuration: config.Config{
				Allowed:         []config.AllowedRule{},
				AllowedSeverity: config.SeverityError,
			},
			wantCount: 1,
		},
		{
			name: "matching rule allows dependency",
			configuration: config.Config{
				Allowed:         []config.AllowedRule{allowCore},
				AllowedSeverity: config.SeverityError,
			},
		},
		{
			name: "later matching rule allows dependency",
			configuration: config.Config{
				Allowed:         []config.AllowedRule{allowOther, allowCore},
				AllowedSeverity: config.SeverityError,
			},
		},
		{
			name: "multiple unmatched named rules use reserved diagnostic name",
			configuration: config.Config{
				Allowed:         []config.AllowedRule{allowOther, allowModule},
				AllowedSeverity: config.SeverityInfo,
			},
			wantCount: 1,
		},
		{
			name: "packageName remains an allowed edge selector",
			configuration: config.Config{
				Allowed: []config.AllowedRule{{
					Name: "allow-app",
					From: config.From{PackageName: []string{`^app$`}},
					To:   config.To{},
				}},
				AllowedSeverity: config.SeverityError,
			},
		},
		{
			name: "ignore allowed severity skips checking",
			configuration: config.Config{
				Allowed:         []config.AllowedRule{},
				AllowedSeverity: config.SeverityIgnore,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Evaluate(&test.configuration, []scanner.File{file})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(got) != test.wantCount {
				t.Fatalf("Evaluate() violation count = %d, want %d: %#v", len(got), test.wantCount, got)
			}
			if test.wantCount == 0 {
				return
			}
			violation := got[0]
			if violation.Rule != NotInAllowedRuleName || violation.Kind != ViolationKindNotAllowed {
				t.Errorf("allowed violation identity = (%q, %q), want (%q, %q)",
					violation.Rule, violation.Kind, NotInAllowedRuleName, ViolationKindNotAllowed)
			}
			wantSeverity := effectiveSeverity(test.configuration.AllowedSeverity)
			if violation.Severity != wantSeverity {
				t.Errorf("allowed violation severity = %q, want %q", violation.Severity, wantSeverity)
			}
			if violation.From.Path != file.Path || violation.From.Line != 3 || violation.From.PackageName != "app" {
				t.Errorf("allowed violation source = %#v, want app.go import metadata", violation.From)
			}
			if violation.To == nil || violation.To.Path != "internal/core" ||
				violation.To.ImportPath != "example.com/project/internal/core" ||
				violation.To.Type != scanner.DependencyTypeLocal {
				t.Errorf("allowed violation dependency = %#v, want raw and resolved metadata", violation.To)
			}
		})
	}
}

func TestCaptureExpansion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filePath  string
		to        config.To
		target    string
		wantCount int
	}{
		{
			name:      "first matching from pattern supplies captures",
			filePath:  "internal/features/alpha/file.go",
			to:        config.To{Path: []string{`^shared/$1$`}},
			target:    "shared/alpha",
			wantCount: 1,
		},
		{
			name:     "captured regexp metacharacters stay literal",
			filePath: "internal/features/team.one/file.go",
			to:       config.To{Path: []string{`^shared/$1$`}},
			target:   "shared/teamXone",
		},
		{
			name:     "capture expands in pathNot",
			filePath: "internal/features/alpha/file.go",
			to:       config.To{PathNot: []string{`^shared/$1$`}},
			target:   "shared/alpha",
		},
		{
			name:      "pathNot permits a different target",
			filePath:  "internal/features/alpha/file.go",
			to:        config.To{PathNot: []string{`^shared/$1$`}},
			target:    "shared/beta",
			wantCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			configuration := config.Config{Forbidden: []config.ForbiddenRule{{
				Name:     "feature-boundary",
				Severity: config.SeverityError,
				From: config.From{Path: []string{
					`^internal/features/([^/]+)/`,
					`^internal/(.+)/`,
				}},
				To: test.to,
			}}}
			file := scanner.File{
				Path:        test.filePath,
				Package:     "feature",
				PackageLine: 1,
				Imports: []scanner.Import{{
					Path:         test.target,
					ResolvedPath: test.target,
					Type:         scanner.DependencyTypeLocal,
					Line:         3,
				}},
			}

			got, err := Evaluate(&configuration, []scanner.File{file})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(got) != test.wantCount {
				t.Errorf("Evaluate() violation count = %d, want %d: %#v", len(got), test.wantCount, got)
			}
		})
	}
}

func TestRequiredDependencies(t *testing.T) {
	t.Parallel()

	files := []scanner.File{
		{
			Path:        "internal/features/alpha/feature.go",
			Package:     "feature",
			PackageLine: 2,
			Imports: []scanner.Import{
				{Path: "fmt", ResolvedPath: "fmt", Type: scanner.DependencyTypeStdlib, Line: 4},
				{
					Path:         "example.com/project/internal/shared/alpha",
					ResolvedPath: "internal/shared/alpha",
					Type:         scanner.DependencyTypeLocal,
					Line:         5,
				},
			},
		},
		{
			Path:        "internal/features/beta/feature.go",
			Package:     "feature",
			PackageLine: 3,
			Imports: []scanner.Import{
				{Path: "fmt", ResolvedPath: "fmt", Type: scanner.DependencyTypeStdlib, Line: 5},
				{Path: "os", ResolvedPath: "os", Type: scanner.DependencyTypeStdlib, Line: 6},
			},
		},
		{
			Path:        "internal/features/gamma/feature.go",
			Package:     "feature",
			PackageLine: 7,
		},
		{
			Path:        "internal/other/other.go",
			Package:     "other",
			PackageLine: 1,
		},
	}
	configuration := config.Config{Required: []config.RequiredRule{
		{
			Name:     "feature-requires-shared",
			Comment:  "import the feature's shared package",
			Severity: config.SeverityError,
			From: config.From{
				Path:        []string{`^internal/features/([^/]+)/`},
				PackageName: []string{`^feature$`},
			},
			To: config.To{
				Path:            []string{`^internal/shared/$1$`},
				DependencyTypes: []config.DependencyType{config.DependencyTypeLocal},
			},
		},
		{
			Name:     "ignored-required",
			Severity: config.SeverityIgnore,
			From:     config.From{Path: []string{"("}},
			To:       config.To{},
		},
	}}

	got, err := Evaluate(&configuration, files)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Evaluate() violations = %#v, want beta and gamma", got)
	}
	wantPaths := []string{
		"internal/features/beta/feature.go",
		"internal/features/gamma/feature.go",
	}
	wantLines := []int{3, 7}
	for index, violation := range got {
		if violation.Rule != "feature-requires-shared" ||
			violation.Comment != "import the feature's shared package" ||
			violation.Severity != config.SeverityError ||
			violation.Kind != ViolationKindRequired ||
			violation.From.Path != wantPaths[index] ||
			violation.From.Line != wantLines[index] ||
			violation.From.PackageName != "feature" ||
			violation.To != nil {
			t.Errorf("required violation[%d] = %#v", index, violation)
		}
	}
}

func TestRequiredCatchAllUsesDefaultSeverity(t *testing.T) {
	t.Parallel()

	configuration := config.Config{Required: []config.RequiredRule{{
		Name: "require-stdlib",
		From: config.From{},
		To: config.To{
			DependencyTypes: []config.DependencyType{config.DependencyTypeStdlib},
		},
	}}}
	files := []scanner.File{
		{
			Path:        "with.go",
			Package:     "sample",
			PackageLine: 1,
			Imports: []scanner.Import{{
				Path: "fmt", ResolvedPath: "fmt", Type: scanner.DependencyTypeStdlib, Line: 3,
			}},
		},
		{Path: "without.go", Package: "sample", PackageLine: 2},
	}

	got, err := Evaluate(&configuration, files)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(got) != 1 || got[0].From.Path != "without.go" ||
		got[0].Severity != config.SeverityWarn || got[0].Kind != ViolationKindRequired {
		t.Errorf("Evaluate() violations = %#v, want one warn required violation for without.go", got)
	}
}

func TestPackageNameDispatch(t *testing.T) {
	t.Parallel()

	imports := []scanner.Import{
		{Path: "fmt", ResolvedPath: "fmt", Type: scanner.DependencyTypeStdlib, Line: 11},
		{Path: "example.net/module", ResolvedPath: "example.net/module", Type: scanner.DependencyTypeModule, Line: 12},
	}
	tests := []struct {
		name           string
		from           config.From
		to             config.To
		imports        []scanner.Import
		wantCount      int
		wantSourceOnly bool
	}{
		{
			name:           "empty to reports importless matching file",
			from:           config.From{PackageName: []string{`^main$`}},
			wantCount:      1,
			wantSourceOnly: true,
		},
		{
			name:           "source-only rule reports once despite multiple imports",
			from:           config.From{PackageName: []string{`^main$`}},
			imports:        imports,
			wantCount:      1,
			wantSourceOnly: true,
		},
		{
			name:      "to selector dispatches packageName as edge rule",
			from:      config.From{PackageName: []string{`^main$`}},
			to:        config.To{DependencyTypes: []config.DependencyType{config.DependencyTypeStdlib}},
			imports:   imports,
			wantCount: 1,
		},
		{
			name:      "pathNot excludes approved main package root",
			from:      config.From{PathNot: []string{`^cmd/`}, PackageName: []string{`^main$`}},
			wantCount: 0,
		},
		{
			name:      "empty catch-all remains an edge rule",
			from:      config.From{},
			imports:   imports[:1],
			wantCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			configuration := config.Config{Forbidden: []config.ForbiddenRule{{
				Name:     "main-placement",
				Severity: config.SeverityError,
				From:     test.from,
				To:       test.to,
			}}}
			file := scanner.File{
				Path:        "cmd/app/main.go",
				Package:     "main",
				PackageLine: 7,
				Imports:     test.imports,
			}

			got, err := Evaluate(&configuration, []scanner.File{file})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(got) != test.wantCount {
				t.Fatalf("Evaluate() violation count = %d, want %d: %#v", len(got), test.wantCount, got)
			}
			if test.wantCount == 0 {
				return
			}
			if (got[0].To == nil) != test.wantSourceOnly {
				t.Errorf("Evaluate() To = %#v, sourceOnly want %t", got[0].To, test.wantSourceOnly)
			}
			if test.wantSourceOnly && got[0].From.Line != 7 {
				t.Errorf("source-only line = %d, want package line 7", got[0].From.Line)
			}
		})
	}
}

func TestOrphanMatchesDisconnectedFiles(t *testing.T) {
	t.Parallel()

	orphan := true
	configuration := config.Config{Forbidden: []config.ForbiddenRule{{
		Name:     "no-orphans",
		Severity: config.SeverityError,
		From:     config.From{Orphan: &orphan},
		To:       config.To{},
	}}}
	files := []scanner.File{
		{
			Path:        "cmd/app/main.go",
			PackagePath: "cmd/app",
			Package:     "main",
			PackageLine: 1,
			Imports: []scanner.Import{{
				Path:         "example.com/project/internal/connected",
				ResolvedPath: "internal/connected",
				Type:         scanner.DependencyTypeLocal,
				Line:         3,
			}},
		},
		{
			Path:        "internal/connected/one.go",
			PackagePath: "internal/connected",
			Package:     "connected",
			PackageLine: 1,
		},
		{
			Path:        "internal/connected/two.go",
			PackagePath: "internal/connected",
			Package:     "connected",
			PackageLine: 2,
		},
		{
			Path:        "internal/self/one.go",
			PackagePath: "internal/self",
			Package:     "self",
			PackageLine: 1,
		},
		{
			Path:        "internal/self/one_test.go",
			PackagePath: "internal/self",
			Package:     "self_test",
			PackageLine: 1,
			Imports: []scanner.Import{{
				Path:         "example.com/project/internal/self",
				ResolvedPath: "internal/self",
				Type:         scanner.DependencyTypeLocal,
				Line:         3,
			}},
		},
		{
			Path:        "internal/lonely/lonely.go",
			PackagePath: "internal/lonely",
			Package:     "lonely",
			PackageLine: 4,
		},
		{
			Path:        "internal/stdlib/user.go",
			PackagePath: "internal/stdlib",
			Package:     "stdlib",
			PackageLine: 1,
			Imports: []scanner.Import{{
				Path: "fmt", ResolvedPath: "fmt", Type: scanner.DependencyTypeStdlib, Line: 3,
			}},
		},
	}

	got, err := Evaluate(&configuration, files)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(got) != 1 || got[0].From.Path != "internal/lonely/lonely.go" || got[0].From.Line != 4 || got[0].To != nil {
		t.Errorf("Evaluate() violations = %#v, want lonely.go package-line source violation", got)
	}
}

func TestCollectFileFactsUsesPackageDependents(t *testing.T) {
	t.Parallel()

	localImport := func(target string) scanner.Import {
		return scanner.Import{
			Path:         "example.com/project/" + target,
			ResolvedPath: target,
			Type:         scanner.DependencyTypeLocal,
		}
	}
	files := []scanner.File{
		{
			Path:        "app/one.go",
			PackagePath: "internal/app",
			Imports:     []scanner.Import{localImport("internal/hub")},
		},
		{
			Path:        "app/two.go",
			PackagePath: "internal/app",
			Imports:     []scanner.Import{localImport("internal/hub")},
		},
		{
			Path:        "other/main.go",
			PackagePath: "internal/other",
			Imports:     []scanner.Import{localImport("internal/hub")},
		},
		{
			Path:        "hub/hub.go",
			PackagePath: "internal/hub",
			Imports:     []scanner.Import{localImport("internal/leaf")},
		},
		{
			Path:        "hub/hub_test.go",
			PackagePath: "internal/hub",
			Package:     "hub_test",
			Imports: []scanner.Import{
				localImport("internal/hub"),
			},
		},
		{Path: "leaf/leaf.go", PackagePath: "internal/leaf"},
		{Path: "lonely/lonely.go", PackagePath: "internal/lonely"},
	}
	facts := collectFileFacts(files, graph.Build(files))
	tests := []struct {
		name                   string
		filePath               string
		wantOrphan             bool
		wantNumberOfDependents int
	}{
		{
			name:                   "distinct importer packages count once",
			filePath:               "hub/hub.go",
			wantNumberOfDependents: 2,
		},
		{
			name:                   "all files share package dependent count",
			filePath:               "hub/hub_test.go",
			wantNumberOfDependents: 2,
		},
		{
			name:                   "imported leaf is not orphan",
			filePath:               "leaf/leaf.go",
			wantNumberOfDependents: 1,
		},
		{
			name:       "disconnected importless file is orphan",
			filePath:   "lonely/lonely.go",
			wantOrphan: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := facts[test.filePath]
			if got.orphan != test.wantOrphan {
				t.Errorf("fileFacts.orphan = %t, want %t", got.orphan, test.wantOrphan)
			}
			if got.numberOfDependents != test.wantNumberOfDependents {
				t.Errorf(
					"fileFacts.numberOfDependents = %d, want %d",
					got.numberOfDependents,
					test.wantNumberOfDependents,
				)
			}
		})
	}
}

func TestNumberOfDependentsMatchesPackageFanIn(t *testing.T) {
	t.Parallel()

	localImport := func(target string, line int) scanner.Import {
		return scanner.Import{
			Path:         "example.com/project/" + target,
			ResolvedPath: target,
			Type:         scanner.DependencyTypeLocal,
			Line:         line,
		}
	}
	files := []scanner.File{
		{
			Path:        "app/one.go",
			PackagePath: "internal/app",
			Imports:     []scanner.Import{localImport("internal/hub", 3)},
		},
		{
			Path:        "app/two.go",
			PackagePath: "internal/app",
			Imports:     []scanner.Import{localImport("internal/hub", 3)},
		},
		{
			Path:        "other/main.go",
			PackagePath: "internal/other",
			Imports:     []scanner.Import{localImport("internal/hub", 3)},
		},
		{
			Path:        "hub/hub.go",
			PackagePath: "internal/hub",
			Package:     "hub",
			PackageLine: 7,
			Imports:     []scanner.Import{localImport("internal/leaf", 11)},
		},
		{
			Path:        "hub/hub_test.go",
			PackagePath: "internal/hub",
			Package:     "hub_test",
			Imports:     []scanner.Import{localImport("internal/hub", 3)},
		},
		{Path: "leaf/leaf.go", PackagePath: "internal/leaf"},
	}
	lessThanTwo := 2
	lessThanThree := 3
	lessThanFour := 4
	moreThanOne := 1
	moreThanTwo := 2
	leafEdge := config.To{
		Path:            []string{`^internal/leaf$`},
		DependencyTypes: []config.DependencyType{config.DependencyTypeLocal},
	}
	tests := []struct {
		name           string
		from           config.From
		to             config.To
		wantCount      int
		wantSourceOnly bool
		wantLine       int
	}{
		{
			name:           "less than alone matches a larger strict bound",
			from:           config.From{NumberOfDependentsLessThan: &lessThanThree},
			wantCount:      1,
			wantSourceOnly: true,
			wantLine:       7,
		},
		{
			name: "less than alone excludes its equality boundary",
			from: config.From{NumberOfDependentsLessThan: &lessThanTwo},
		},
		{
			name:           "more than alone matches a smaller strict bound",
			from:           config.From{NumberOfDependentsMoreThan: &moreThanOne},
			wantCount:      1,
			wantSourceOnly: true,
			wantLine:       7,
		},
		{
			name: "same-directory external test import does not cross more than boundary",
			from: config.From{NumberOfDependentsMoreThan: &moreThanTwo},
		},
		{
			name: "open range matches between both bounds",
			from: config.From{
				NumberOfDependentsLessThan: &lessThanThree,
				NumberOfDependentsMoreThan: &moreThanOne,
			},
			wantCount:      1,
			wantSourceOnly: true,
			wantLine:       7,
		},
		{
			name: "open range excludes its lower equality boundary",
			from: config.From{
				NumberOfDependentsLessThan: &lessThanFour,
				NumberOfDependentsMoreThan: &moreThanTwo,
			},
		},
		{
			name:      "edge condition combines with matching from count",
			from:      config.From{NumberOfDependentsMoreThan: &moreThanOne},
			to:        leafEdge,
			wantCount: 1,
			wantLine:  11,
		},
		{
			name: "edge condition cannot bypass a nonmatching from count",
			from: config.From{NumberOfDependentsMoreThan: &moreThanTwo},
			to:   leafEdge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			test.from.Path = []string{`^hub/hub\.go$`}
			configuration := config.Config{Forbidden: []config.ForbiddenRule{{
				Name:     "dependent-count",
				Severity: config.SeverityError,
				From:     test.from,
				To:       test.to,
			}}}
			got, err := Evaluate(&configuration, files)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if len(got) != test.wantCount {
				t.Fatalf("Evaluate() violation count = %d, want %d: %#v", len(got), test.wantCount, got)
			}
			if test.wantCount == 0 {
				return
			}
			if (got[0].To == nil) != test.wantSourceOnly {
				t.Errorf("Evaluate() To = %#v, sourceOnly want %t", got[0].To, test.wantSourceOnly)
			}
			if got[0].From.Line != test.wantLine {
				t.Errorf("Evaluate() source line = %d, want %d", got[0].From.Line, test.wantLine)
			}
		})
	}
}

func TestForbiddenDuplicationAndIgnore(t *testing.T) {
	t.Parallel()

	matchingRule := func(name string) config.ForbiddenRule {
		return config.ForbiddenRule{
			Name:     name,
			Comment:  "move the import behind an adapter",
			Severity: config.SeverityError,
			From:     config.From{Path: []string{`^internal/`, `app\.go$`}},
			To: config.To{
				Path: []string{`^internal/core$`, `core$`},
			},
		}
	}
	file := scanner.File{
		Path:        "internal/app/app.go",
		Package:     "app",
		PackageLine: 1,
		Imports: []scanner.Import{{
			Path:         "example.com/project/internal/core",
			ResolvedPath: "internal/core",
			Type:         scanner.DependencyTypeLocal,
			Line:         3,
		}},
	}

	tests := []struct {
		name      string
		rules     []config.ForbiddenRule
		wantRules []string
	}{
		{
			name:      "different matching rules remain distinct",
			rules:     []config.ForbiddenRule{matchingRule("z-rule"), matchingRule("a-rule")},
			wantRules: []string{"a-rule", "z-rule"},
		},
		{
			name:      "multiple matching patterns in one rule report once",
			rules:     []config.ForbiddenRule{matchingRule("one-rule")},
			wantRules: []string{"one-rule"},
		},
		{
			name: "ignore rule is not evaluated",
			rules: []config.ForbiddenRule{{
				Name:     "ignored",
				Severity: config.SeverityIgnore,
				From:     config.From{Path: []string{"("}},
				To:       config.To{},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Evaluate(&config.Config{Forbidden: test.rules}, []scanner.File{file})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			gotRules := make([]string, 0, len(got))
			for _, violation := range got {
				gotRules = append(gotRules, violation.Rule)
			}
			if !slices.Equal(gotRules, test.wantRules) {
				t.Errorf("Evaluate() rule names = %q, want %q", gotRules, test.wantRules)
			}
			if len(got) > 0 && (got[0].Comment == "" || got[0].Kind != ViolationKindForbidden) {
				t.Errorf("forbidden violation metadata = %#v, want comment and kind", got[0])
			}
		})
	}
}

func TestSelectorFieldsUseORWithinAndANDBetween(t *testing.T) {
	t.Parallel()

	configuration := config.Config{Forbidden: []config.ForbiddenRule{{
		Name:     "selected",
		Severity: config.SeverityError,
		From: config.From{
			Path:        []string{`^internal/`, `^cmd/`},
			PathNot:     []string{`generated`},
			PackageName: []string{`^app$`},
		},
		To: config.To{
			Path:               []string{`^internal/`},
			PathNot:            []string{`/allowed$`},
			DependencyTypes:    []config.DependencyType{config.DependencyTypeLocal},
			DependencyTypesNot: []config.DependencyType{config.DependencyTypeModule},
		},
	}}}
	imports := []scanner.Import{
		{Path: "project/internal/bad", ResolvedPath: "internal/bad", Type: scanner.DependencyTypeLocal, Line: 3},
		{Path: "project/internal/allowed", ResolvedPath: "internal/allowed", Type: scanner.DependencyTypeLocal, Line: 4},
		{Path: "example.net/module", ResolvedPath: "example.net/module", Type: scanner.DependencyTypeModule, Line: 5},
	}
	files := []scanner.File{
		{Path: "internal/app/app.go", Package: "app", PackageLine: 1, Imports: imports},
		{Path: "internal/generated/app.go", Package: "app", PackageLine: 1, Imports: imports[:1]},
		{Path: "internal/other/app.go", Package: "other", PackageLine: 1, Imports: imports[:1]},
	}

	got, err := Evaluate(&configuration, files)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(got) != 1 || got[0].To == nil || got[0].To.Path != "internal/bad" {
		t.Errorf("Evaluate() violations = %#v, want only internal/bad", got)
	}
}

func TestUnresolvedDependencyUsesRawImportPath(t *testing.T) {
	t.Parallel()

	configuration := config.Config{Forbidden: []config.ForbiddenRule{{
		Name:     "no-unresolved-cgo",
		Severity: config.SeverityError,
		From:     config.From{},
		To: config.To{
			Path:            []string{`^C$`},
			DependencyTypes: []config.DependencyType{config.DependencyTypeUnresolved},
		},
	}}}
	file := scanner.File{
		Path:        "internal/cgo/cgo.go",
		Package:     "cgo",
		PackageLine: 1,
		Imports: []scanner.Import{{
			Path: "C", Type: scanner.DependencyTypeUnresolved, Line: 3,
		}},
	}

	got, err := Evaluate(&configuration, []scanner.File{file})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(got) != 1 || got[0].To == nil || got[0].To.Path != "C" || got[0].To.ImportPath != "C" {
		t.Errorf("Evaluate() violations = %#v, want raw unresolved import path C", got)
	}
}

func TestEvaluateRejectsInvalidProgrammaticConfiguration(t *testing.T) {
	t.Parallel()

	file := scanner.File{
		Path:        "internal/app/app.go",
		Package:     "app",
		PackageLine: 1,
		Imports: []scanner.Import{{
			Path: "fmt", ResolvedPath: "fmt", Type: scanner.DependencyTypeStdlib, Line: 3,
		}},
	}
	tests := []struct {
		name          string
		configuration *config.Config
		wantError     string
	}{
		{name: "nil configuration", wantError: "configuration is nil"},
		{
			name: "invalid from regexp",
			configuration: &config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "invalid-from", From: config.From{Path: []string{"("}}, To: config.To{},
			}}},
			wantError: "from.path",
		},
		{
			name: "invalid to regexp",
			configuration: &config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "invalid-to", From: config.From{}, To: config.To{Path: []string{"("}},
			}}},
			wantError: "expanded",
		},
		{
			name: "unavailable capture",
			configuration: &config.Config{Forbidden: []config.ForbiddenRule{{
				Name: "invalid-capture", From: config.From{}, To: config.To{Path: []string{`$1`}},
			}}},
			wantError: "unavailable",
		},
		{
			name: "invalid required from regexp",
			configuration: &config.Config{Required: []config.RequiredRule{{
				Name: "invalid-required-from", From: config.From{Path: []string{"("}},
				To: config.To{Path: []string{`^fmt$`}},
			}}},
			wantError: "required[0]",
		},
		{
			name: "invalid required to regexp",
			configuration: &config.Config{Required: []config.RequiredRule{{
				Name: "invalid-required-to", From: config.From{},
				To: config.To{Path: []string{"("}},
			}}},
			wantError: "required rule",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Evaluate(test.configuration, []scanner.File{file})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Errorf("Evaluate() error = %v, want text %q", err, test.wantError)
			}
		})
	}
}

func TestNotInAllowedRuleNameIsReservedByLoader(t *testing.T) {
	t.Parallel()

	data := []byte(`{"allowed":[{"name":"` + NotInAllowedRuleName + `","from":{},"to":{}}]}`)
	_, err := config.Parse(data)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("config.Parse() error = %v, want reserved-name error", err)
	}
}
