package baseline_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func TestApplyThreeStates(t *testing.T) {
	t.Parallel()

	current := violation("no-third-party", "internal/core/core.go", "example.com/lib", config.SeverityWarn)
	entry := baseline.Entry{
		Rule: "no-third-party",
		From: "internal/core/core.go",
		To:   stringPointer("example.com/lib"),
	}

	tests := []struct {
		name     string
		baseline baseline.Baseline
		current  []engine.Violation
		want     baseline.Result
	}{
		{
			name:    "new violation preserves configured severity",
			current: []engine.Violation{current},
			want: baseline.Result{
				Violations: []engine.Violation{current},
			},
		},
		{
			name:     "known violation is suppressed separately",
			baseline: baseline.Baseline{Entries: []baseline.Entry{entry}},
			current:  []engine.Violation{current},
			want: baseline.Result{
				Known: []engine.Violation{current},
			},
		},
		{
			name:     "missing violation makes baseline stale",
			baseline: baseline.Baseline{Entries: []baseline.Entry{entry}},
			want: baseline.Result{
				Stale: []baseline.StaleError{{Entry: entry}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := baseline.Apply(test.baseline, test.current)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Apply() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestApplyUsesOnlyExactIdentityKeys(t *testing.T) {
	t.Parallel()

	baseEntry := baseline.Entry{
		Rule: "rule",
		From: "from.go",
		To:   stringPointer("example.com/raw"),
	}
	baseViolation := violation("rule", "from.go", "example.com/raw", config.SeverityInfo)
	baseViolation.Comment = "changed comment"
	baseViolation.Kind = engine.ViolationKindNotAllowed
	baseViolation.From.Line = 99
	baseViolation.From.PackageName = "changedpackage"
	baseViolation.To.Path = "resolved/path/changed"
	baseViolation.To.Type = scanner.DependencyTypeUnresolved

	tests := []struct {
		name      string
		entry     baseline.Entry
		violation engine.Violation
		wantKnown bool
	}{
		{
			name:      "non-key metadata does not affect match",
			entry:     baseEntry,
			violation: baseViolation,
			wantKnown: true,
		},
		{
			name:      "different rule does not match",
			entry:     baseline.Entry{Rule: "other", From: baseEntry.From, To: baseEntry.To},
			violation: baseViolation,
		},
		{
			name:      "different from path does not match",
			entry:     baseline.Entry{Rule: baseEntry.Rule, From: "other.go", To: baseEntry.To},
			violation: baseViolation,
		},
		{
			name:      "resolved path cannot replace raw import path",
			entry:     baseline.Entry{Rule: baseEntry.Rule, From: baseEntry.From, To: stringPointer(baseViolation.To.Path)},
			violation: baseViolation,
		},
		{
			name:      "source-only exact match",
			entry:     baseline.Entry{Rule: "source-rule", From: "orphan.go"},
			violation: sourceViolation("source-rule", "orphan.go"),
			wantKnown: true,
		},
		{
			name:      "source-only and edge identities differ",
			entry:     baseline.Entry{Rule: baseEntry.Rule, From: baseEntry.From},
			violation: baseViolation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := baseline.Apply(
				baseline.Baseline{Entries: []baseline.Entry{test.entry}},
				[]engine.Violation{test.violation},
			)
			if test.wantKnown {
				if !reflect.DeepEqual(got.Known, []engine.Violation{test.violation}) {
					t.Fatalf("Known = %#v, want current violation", got.Known)
				}
				if len(got.Violations) != 0 || len(got.Stale) != 0 {
					t.Fatalf("matched result = %#v, want only Known", got)
				}
				return
			}

			if !reflect.DeepEqual(got.Violations, []engine.Violation{test.violation}) {
				t.Fatalf("Violations = %#v, want current violation", got.Violations)
			}
			if len(got.Known) != 0 || !reflect.DeepEqual(got.Stale, []baseline.StaleError{{Entry: test.entry}}) {
				t.Fatalf("unmatched result = %#v, want violation and stale entry", got)
			}
		})
	}
}

func TestGenerateStableSortAndDedupe(t *testing.T) {
	t.Parallel()

	duplicate := violation("z-rule", "b.go", "z.example/lib", config.SeverityError)
	duplicate.Comment = "metadata differs but identity is equal"
	duplicate.From.Line = 42
	duplicate.To.Path = "another/resolved/path"
	duplicate.To.Type = scanner.DependencyTypeLocal

	got := baseline.Generate([]engine.Violation{
		violation("z-rule", "b.go", "z.example/lib", config.SeverityWarn),
		violation("a-rule", "c.go", "b.example/lib", config.SeverityWarn),
		sourceViolation("a-rule", "c.go"),
		violation("a-rule", "a.go", "z.example/lib", config.SeverityWarn),
		duplicate,
	})
	want := baseline.Baseline{Entries: []baseline.Entry{
		{Rule: "a-rule", From: "a.go", To: stringPointer("z.example/lib")},
		{Rule: "a-rule", From: "c.go"},
		{Rule: "a-rule", From: "c.go", To: stringPointer("b.example/lib")},
		{Rule: "z-rule", From: "b.go", To: stringPointer("z.example/lib")},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Generate() = %#v, want %#v", got, want)
	}
}

func TestStaleErrorMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry baseline.Entry
		want  string
	}{
		{
			name:  "edge",
			entry: baseline.Entry{Rule: "rule", From: "from.go", To: stringPointer("example.com/lib")},
			want:  `baseline entry is stale: rule "rule", from "from.go", to "example.com/lib"; remove this entry from the baseline.`,
		},
		{
			name:  "source only",
			entry: baseline.Entry{Rule: "orphan", From: "orphan.go"},
			want:  `baseline entry is stale: rule "orphan", from "orphan.go"; remove this entry from the baseline.`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var err error = baseline.StaleError{Entry: test.entry}
			if !errors.As(err, new(baseline.StaleError)) {
				t.Fatal("StaleError does not implement error")
			}
			if got := err.Error(); got != test.want {
				t.Fatalf("Error() = %q, want %q", got, test.want)
			}
		})
	}
}

func violation(rule, from, to string, severity config.Severity) engine.Violation {
	return engine.Violation{
		Rule:     rule,
		Comment:  "comment",
		Severity: severity,
		Kind:     engine.ViolationKindForbidden,
		From: engine.Source{
			Path:        from,
			Line:        7,
			PackageName: "sample",
		},
		To: &engine.Dependency{
			Path:       "resolved/" + to,
			ImportPath: to,
			Type:       scanner.DependencyTypeModule,
		},
	}
}

func sourceViolation(rule, from string) engine.Violation {
	return engine.Violation{
		Rule:     rule,
		Comment:  "source-only comment",
		Severity: config.SeverityWarn,
		Kind:     engine.ViolationKindForbidden,
		From: engine.Source{
			Path:        from,
			Line:        3,
			PackageName: "sample",
		},
	}
}

func stringPointer(value string) *string {
	return &value
}
