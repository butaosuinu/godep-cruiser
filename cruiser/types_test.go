package cruiser_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/butaosuinu/godep-cruiser/config"
	"github.com/butaosuinu/godep-cruiser/cruiser"
	internalbaseline "github.com/butaosuinu/godep-cruiser/internal/baseline"
	"github.com/butaosuinu/godep-cruiser/internal/engine"
	"github.com/butaosuinu/godep-cruiser/internal/scanner"
)

func TestAliasesUseCanonicalTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		alias     reflect.Type
		canonical reflect.Type
	}{
		{name: "dependency type", alias: reflect.TypeFor[scanner.DependencyType](), canonical: reflect.TypeFor[config.DependencyType]()},
		{name: "violation kind", alias: reflect.TypeFor[cruiser.ViolationKind](), canonical: reflect.TypeFor[engine.ViolationKind]()},
		{name: "source", alias: reflect.TypeFor[cruiser.Source](), canonical: reflect.TypeFor[engine.Source]()},
		{name: "dependency", alias: reflect.TypeFor[cruiser.Dependency](), canonical: reflect.TypeFor[engine.Dependency]()},
		{name: "violation", alias: reflect.TypeFor[cruiser.Violation](), canonical: reflect.TypeFor[engine.Violation]()},
		{name: "baseline", alias: reflect.TypeFor[cruiser.Baseline](), canonical: reflect.TypeFor[internalbaseline.Baseline]()},
		{name: "baseline entry", alias: reflect.TypeFor[cruiser.BaselineEntry](), canonical: reflect.TypeFor[internalbaseline.Entry]()},
		{name: "stale error", alias: reflect.TypeFor[cruiser.StaleError](), canonical: reflect.TypeFor[internalbaseline.StaleError]()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.alias != test.canonical {
				t.Errorf("alias type = %v, want canonical type %v", test.alias, test.canonical)
			}
		})
	}
}

func TestViolationKindJSONValuesRemainStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind cruiser.ViolationKind
		want string
	}{
		{name: "forbidden", kind: cruiser.ViolationKindForbidden, want: "forbidden"},
		{name: "not in allowed", kind: cruiser.ViolationKindNotAllowed, want: "not-in-allowed"},
		{name: "required", kind: cruiser.ViolationKindRequired, want: "required"},
		{name: "reachable", kind: cruiser.ViolationKindReachable, want: "reachable"},
		{name: "unreachable", kind: cruiser.ViolationKindUnreachable, want: "unreachable"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			result := cruiser.Result{Violations: []cruiser.Violation{{Kind: test.kind}}}
			if err := cruiser.WriteReport(&output, cruiser.OutputTypeJSON, result); err != nil {
				t.Fatalf("cruiser.WriteReport() error = %v", err)
			}

			var report struct {
				Violations []struct {
					Kind string `json:"kind"`
				} `json:"violations"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if len(report.Violations) != 1 || report.Violations[0].Kind != test.want {
				t.Errorf("JSON violations = %#v, want kind %q", report.Violations, test.want)
			}
		})
	}
}
