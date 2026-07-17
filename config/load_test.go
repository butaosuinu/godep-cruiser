package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLoadValidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, got *Config)
	}{
		{
			name: "all schema fields",
			input: `{
  "forbidden": [
    {
      "name": "all-fields",
      "comment": "covers every matcher",
      "severity": "error",
      "scope": "module",
      "from": {
        "path": ["^cmd/([^/]+)/"],
        "pathNot": ["_test\\.go$"],
        "orphan": false,
        "packageName": ["^main$"],
        "numberOfDependentsLessThan": 2,
        "numberOfDependentsMoreThan": 0
      },
      "to": {
        "path": ["^internal/$1/"],
        "pathNot": ["^vendor/$1/"],
        "moreUnstable": true,
        "dependencyTypes": ["stdlib", "local", "module", "unresolved"],
        "dependencyTypesNot": ["unresolved"]
      }
    },
    {"name": "warn", "severity": "warn", "from": {}, "to": {}},
    {"name": "info", "severity": "info", "from": {}, "to": {}},
    {"name": "ignore", "severity": "ignore", "from": {}, "to": {}}
  ],
  "required": [
    {
      "name": "require-feature-shared",
      "comment": "each feature imports its shared package",
      "severity": "error",
      "from": {
        "path": ["^internal/features/([^/]+)/"],
        "pathNot": ["_test\\.go$"],
        "packageName": ["^feature$"]
      },
      "to": {
        "path": ["^internal/shared/$1$"],
        "pathNot": ["/legacy$"],
        "dependencyTypes": ["local"],
        "dependencyTypesNot": ["unresolved"]
      }
    },
    {"name": "require-stdlib", "from": {}, "to": {"dependencyTypes": ["stdlib"]}}
  ],
  "allowed": [
    {
      "name": "allow-local",
      "comment": "allowed rule metadata",
      "from": {"path": ["^config/"]},
      "to": {"dependencyTypes": ["stdlib", "local"]}
    }
  ],
  "allowedSeverity": "info"
}`,
			check: func(t *testing.T, got *Config) {
				t.Helper()
				if len(got.Forbidden) != 4 {
					t.Fatalf("len(Forbidden) = %d, want 4", len(got.Forbidden))
				}
				allFields := got.Forbidden[0]
				if allFields.Name != "all-fields" || allFields.Comment != "covers every matcher" {
					t.Errorf("forbidden metadata = %#v", allFields)
				}
				if allFields.Severity != SeverityError {
					t.Errorf("Severity = %q, want %q", allFields.Severity, SeverityError)
				}
				if allFields.Scope != ScopeModule {
					t.Errorf("Scope = %q, want %q", allFields.Scope, ScopeModule)
				}
				wantSeverities := []Severity{SeverityError, SeverityWarn, SeverityInfo, SeverityIgnore}
				for index, want := range wantSeverities {
					if got.Forbidden[index].Severity != want {
						t.Errorf("Forbidden[%d].Severity = %q, want %q", index, got.Forbidden[index].Severity, want)
					}
				}
				if allFields.From.Orphan == nil || *allFields.From.Orphan {
					t.Errorf("From.Orphan = %v, want pointer to false", allFields.From.Orphan)
				}
				if len(allFields.From.Path) != 1 || len(allFields.From.PathNot) != 1 || len(allFields.From.PackageName) != 1 {
					t.Errorf("From regex fields not loaded: %#v", allFields.From)
				}
				if allFields.From.NumberOfDependentsLessThan == nil ||
					*allFields.From.NumberOfDependentsLessThan != 2 ||
					allFields.From.NumberOfDependentsMoreThan == nil ||
					*allFields.From.NumberOfDependentsMoreThan != 0 {
					t.Errorf("From dependent-count fields not loaded: %#v", allFields.From)
				}
				if len(allFields.To.Path) != 1 || len(allFields.To.PathNot) != 1 {
					t.Errorf("To regex fields not loaded: %#v", allFields.To)
				}
				if len(allFields.To.DependencyTypes) != 4 || len(allFields.To.DependencyTypesNot) != 1 {
					t.Errorf("dependency types not loaded: %#v", allFields.To)
				}
				if allFields.To.MoreUnstable == nil || !*allFields.To.MoreUnstable {
					t.Errorf("MoreUnstable = %v, want pointer to true", allFields.To.MoreUnstable)
				}
				if len(got.Allowed) != 1 || got.Allowed[0].Name != "allow-local" {
					t.Errorf("Allowed = %#v", got.Allowed)
				} else {
					allowed := got.Allowed[0]
					if allowed.Comment != "allowed rule metadata" || len(allowed.From.Path) != 1 || allowed.From.Path[0] != "^config/" {
						t.Errorf("allowed metadata/from fields not loaded: %#v", allowed)
					}
					if len(allowed.To.DependencyTypes) != 2 ||
						allowed.To.DependencyTypes[0] != DependencyTypeStdlib ||
						allowed.To.DependencyTypes[1] != DependencyTypeLocal {
						t.Errorf("allowed to fields not loaded: %#v", allowed.To)
					}
				}
				if len(got.Required) != 2 {
					t.Fatalf("len(Required) = %d, want 2", len(got.Required))
				}
				required := got.Required[0]
				if required.Name != "require-feature-shared" ||
					required.Comment != "each feature imports its shared package" ||
					required.Severity != SeverityError ||
					len(required.From.Path) != 1 || len(required.From.PathNot) != 1 ||
					len(required.From.PackageName) != 1 || required.From.Orphan != nil ||
					len(required.To.Path) != 1 || len(required.To.PathNot) != 1 ||
					len(required.To.DependencyTypes) != 1 || len(required.To.DependencyTypesNot) != 1 {
					t.Errorf("required fields not loaded: %#v", required)
				}
				if got.Required[1].Severity != SeverityWarn {
					t.Errorf("default required severity = %q, want %q", got.Required[1].Severity, SeverityWarn)
				}
				if got.AllowedSeverity != SeverityInfo {
					t.Errorf("AllowedSeverity = %q, want %q", got.AllowedSeverity, SeverityInfo)
				}
			},
		},
		{
			name:  "explicit catch-all and defaults",
			input: `{"forbidden":[{"name":"deny-all","from":{},"to":{}}]}`,
			check: func(t *testing.T, got *Config) {
				t.Helper()
				if got.Forbidden[0].Severity != SeverityWarn {
					t.Errorf("default Severity = %q, want %q", got.Forbidden[0].Severity, SeverityWarn)
				}
				if got.Forbidden[0].Scope != ScopeModule {
					t.Errorf("default Scope = %q, want %q", got.Forbidden[0].Scope, ScopeModule)
				}
				if got.Forbidden[0].To.Reachable != nil {
					t.Errorf("default Reachable = %v, want nil", got.Forbidden[0].To.Reachable)
				}
				if got.Forbidden[0].To.MoreUnstable != nil {
					t.Errorf("default MoreUnstable = %v, want nil", got.Forbidden[0].To.MoreUnstable)
				}
				if got.AllowedSeverity != SeverityWarn {
					t.Errorf("default AllowedSeverity = %q, want %q", got.AllowedSeverity, SeverityWarn)
				}
			},
		},
		{
			name: "folder scope allows package fan-in conditions",
			input: `{
  "forbidden": [{
    "name": "folder-fan-in",
    "scope": "folder",
    "from": {
      "numberOfDependentsLessThan": 3,
      "numberOfDependentsMoreThan": 0
    },
    "to": {"moreUnstable": true}
  }]
}`,
			check: func(t *testing.T, got *Config) {
				t.Helper()
				rule := got.Forbidden[0]
				if rule.Scope != ScopeFolder {
					t.Errorf("Scope = %q, want %q", rule.Scope, ScopeFolder)
				}
				if rule.From.NumberOfDependentsLessThan == nil ||
					*rule.From.NumberOfDependentsLessThan != 3 ||
					rule.From.NumberOfDependentsMoreThan == nil ||
					*rule.From.NumberOfDependentsMoreThan != 0 {
					t.Errorf("folder fan-in conditions = %#v, want less than 3 and more than 0", rule.From)
				}
				if rule.To.MoreUnstable == nil || !*rule.To.MoreUnstable {
					t.Errorf("folder MoreUnstable = %v, want pointer to true", rule.To.MoreUnstable)
				}
			},
		},
		{
			name: "folder scope allows capture expansion",
			input: `{
  "forbidden": [{
    "name": "folder-capture",
    "scope": "folder",
    "from": {"path": ["^internal/([^/]+)$"]},
    "to": {"path": ["^internal/$1/api$"]}
  }]
}`,
			check: func(t *testing.T, got *Config) {
				t.Helper()
				rule := got.Forbidden[0]
				if rule.Scope != ScopeFolder || rule.To.Path[0] != "^internal/$1/api$" {
					t.Errorf("folder capture rule = %#v", rule)
				}
			},
		},
		{
			name:  "present empty allowed list stays enabled",
			input: `{"allowed":[]}`,
			check: func(t *testing.T, got *Config) {
				t.Helper()
				if got.Allowed == nil || len(got.Allowed) != 0 {
					t.Errorf("Allowed = %#v, want non-nil empty slice", got.Allowed)
				}
			},
		},
		{
			name: "escaped dollar is a literal regex token",
			input: `{
  "forbidden": [{
    "name": "literal-dollar",
    "from": {},
    "to": {"path": ["^literal/\\$1$"]}
  }]
}`,
			check: func(t *testing.T, got *Config) {
				t.Helper()
				if got.Forbidden[0].To.Path[0] != `^literal/\$1$` {
					t.Errorf("literal-dollar pattern = %q", got.Forbidden[0].To.Path[0])
				}
			},
		},
		{
			name:  "capture template remains valid when a capture can be empty",
			input: captureRule(`"^(.*)foo\\.go$"`, `"^$1*$"`),
			check: func(t *testing.T, _ *Config) {
				t.Helper()
			},
		},
		{
			name:  "capture after a negated caret class is outside the class",
			input: captureRule(`"^(.*)$"`, `"[^^]$1"`),
			check: func(t *testing.T, _ *Config) {
				t.Helper()
			},
		},
		{
			name: "reachable true allows captures and pathNot",
			input: `{
  "forbidden": [{
    "name": "reachable-capture",
    "from": {"path": ["^cmd/([^/]+)/"]},
    "to": {
      "path": ["^internal/$1/"],
      "pathNot": ["^internal/$1/allowed$"],
      "reachable": true
    }
  }]
}`,
			check: func(t *testing.T, got *Config) {
				t.Helper()
				reachable := got.Forbidden[0].To.Reachable
				if reachable == nil || !*reachable {
					t.Errorf("Reachable = %v, want pointer to true", reachable)
				}
				if len(got.Forbidden[0].To.PathNot) != 1 {
					t.Errorf("PathNot = %#v, want one exclusion", got.Forbidden[0].To.PathNot)
				}
			},
		},
		{
			name: "reachable false allows plain pathNot",
			input: `{
  "forbidden": [{
    "name": "unreachable",
    "from": {"path": ["^cmd/"]},
    "to": {
      "path": ["^internal/"],
      "pathNot": ["/generated$"],
      "reachable": false
    }
  }]
}`,
			check: func(t *testing.T, got *Config) {
				t.Helper()
				reachable := got.Forbidden[0].To.Reachable
				if reachable == nil || *reachable {
					t.Errorf("Reachable = %v, want pointer to false", reachable)
				}
				if len(got.Forbidden[0].To.PathNot) != 1 {
					t.Errorf("PathNot = %#v, want one exclusion", got.Forbidden[0].To.PathNot)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := Load(strings.NewReader(test.input))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			test.check(t, got)
		})
	}
}

func TestLoadRejectsInvalidConfigurationWithPosition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantPath string
		wantText string
	}{
		{name: "malformed JSON", input: `{"forbidden":`, wantText: "unexpected EOF"},
		{name: "YAML is not accepted", input: "forbidden:\n  - name: nope", wantText: "invalid character"},
		{name: "root must be object", input: `[]`, wantPath: "$", wantText: "must be an object"},
		{name: "unknown top-level field", input: `{"scope":"module"}`, wantPath: "$.scope", wantText: `unknown field "scope"`},
		{name: "unknown field JSON path is quoted", input: `{"bad.field":true}`, wantPath: `$["bad.field"]`, wantText: `unknown field "bad.field"`},
		{name: "duplicate top-level field", input: `{"allowed":[],"allowed":[]}`, wantPath: "$.allowed", wantText: `duplicate field "allowed"`},
		{name: "duplicate forbidden rule name", input: `{"forbidden":[{"name":"dup","from":{},"to":{}},{"name":"dup","from":{},"to":{}}]}`, wantPath: "$.forbidden[1].name", wantText: `duplicate rule name "dup"`},
		{name: "duplicate required rule name", input: `{"required":[{"name":"dup","from":{},"to":{"path":["a"]}},{"name":"dup","from":{},"to":{"path":["b"]}}]}`, wantPath: "$.required[1].name", wantText: `duplicate rule name "dup"`},
		{name: "forbidden and required names collide", input: `{"forbidden":[{"name":"dup","from":{},"to":{}}],"required":[{"name":"dup","from":{},"to":{"path":["a"]}}]}`, wantPath: "$.required[0].name", wantText: `duplicate rule name "dup"`},
		{name: "duplicate allowed rule name", input: `{"allowed":[{"name":"dup","from":{},"to":{}},{"name":"dup","from":{},"to":{}}]}`, wantPath: "$.allowed[1].name", wantText: `duplicate rule name "dup"`},
		{name: "reserved forbidden rule name", input: `{"forbidden":[{"name":"not-in-allowed","from":{},"to":{}}]}`, wantPath: "$.forbidden[0].name", wantText: `rule name "not-in-allowed" is reserved`},
		{name: "reserved required rule name", input: `{"required":[{"name":"not-in-allowed","from":{},"to":{"path":["a"]}}]}`, wantPath: "$.required[0].name", wantText: `rule name "not-in-allowed" is reserved`},
		{name: "reserved allowed rule name", input: `{"allowed":[{"name":"not-in-allowed","from":{},"to":{}}]}`, wantPath: "$.allowed[0].name", wantText: `rule name "not-in-allowed" is reserved`},
		{name: "forbidden must be array", input: `{"forbidden":null}`, wantPath: "$.forbidden", wantText: "must be an array"},
		{name: "required must be array", input: `{"required":null}`, wantPath: "$.required", wantText: "must be an array"},
		{name: "empty rule", input: `{"forbidden":[{}]}`, wantPath: "$.forbidden[0]", wantText: `missing required field "name"`},
		{name: "empty name", input: validRule(`"name":""`), wantPath: "$.forbidden[0].name", wantText: "must not be empty"},
		{name: "missing from", input: `{"forbidden":[{"name":"x","to":{}}]}`, wantPath: "$.forbidden[0]", wantText: `missing required field "from"`},
		{name: "missing to", input: `{"forbidden":[{"name":"x","from":{}}]}`, wantPath: "$.forbidden[0]", wantText: `missing required field "to"`},
		{name: "unknown rule field", input: validRule(`"unexpected":true`), wantPath: "$.forbidden[0].unexpected", wantText: `unknown field "unexpected"`},
		{name: "allowed scope is unsupported", input: `{"allowed":[{"name":"x","scope":"module","from":{},"to":{}}]}`, wantPath: "$.allowed[0].scope", wantText: `unknown field "scope"`},
		{name: "required scope is unsupported", input: `{"required":[{"name":"x","scope":"module","from":{},"to":{"path":["a"]}}]}`, wantPath: "$.required[0].scope", wantText: `unknown field "scope"`},
		{name: "scope must be string", input: validRule(`"scope":null`), wantPath: "$.forbidden[0].scope", wantText: "must be a string"},
		{name: "unknown scope", input: validRule(`"scope":"package"`), wantPath: "$.forbidden[0].scope", wantText: `unknown scope "package"`},
		{name: "folder scope rejects orphan", input: `{"forbidden":[{"name":"x","scope":"folder","from":{"orphan":false},"to":{}}]}`, wantPath: "$.forbidden[0].from.orphan", wantText: `cannot be combined with scope "folder"`},
		{name: "folder scope rejects package name", input: `{"forbidden":[{"name":"x","scope":"folder","from":{"packageName":["main"]},"to":{}}]}`, wantPath: "$.forbidden[0].from.packageName", wantText: `cannot be combined with scope "folder"`},
		{name: "folder scope rejects reachable", input: `{"forbidden":[{"name":"x","scope":"folder","from":{},"to":{"path":["a"],"reachable":true}}]}`, wantPath: "$.forbidden[0].to.reachable", wantText: `cannot be combined with scope "folder"`},
		{name: "folder scope rejects dependency types", input: `{"forbidden":[{"name":"x","scope":"folder","from":{},"to":{"dependencyTypes":["local"]}}]}`, wantPath: "$.forbidden[0].to.dependencyTypes", wantText: `cannot be combined with scope "folder"`},
		{name: "folder scope rejects excluded dependency types", input: `{"forbidden":[{"name":"x","scope":"folder","from":{},"to":{"dependencyTypesNot":["unresolved"]}}]}`, wantPath: "$.forbidden[0].to.dependencyTypesNot", wantText: `cannot be combined with scope "folder"`},
		{name: "allowed rule severity is unsupported", input: `{"allowed":[{"name":"x","severity":"error","from":{},"to":{}}]}`, wantPath: "$.allowed[0].severity", wantText: `unknown field "severity"`},
		{name: "required to must define a condition", input: `{"required":[{"name":"x","from":{},"to":{}}]}`, wantPath: "$.required[0].to", wantText: "must define at least one condition"},
		{name: "required from orphan is unsupported", input: `{"required":[{"name":"x","from":{"orphan":false},"to":{"path":["a"]}}]}`, wantPath: "$.required[0].from.orphan", wantText: `unknown field "orphan"`},
		{name: "required from dependent count is unsupported", input: `{"required":[{"name":"x","from":{"numberOfDependentsMoreThan":0},"to":{"path":["a"]}}]}`, wantPath: "$.required[0].from.numberOfDependentsMoreThan", wantText: `unknown field "numberOfDependentsMoreThan"`},
		{name: "unknown from field", input: validRule(`"from":{"reachable":true}`), wantPath: "$.forbidden[0].from.reachable", wantText: `unknown field "reachable"`},
		{name: "unknown to field", input: validRule(`"to":{"couldNotResolve":true}`), wantPath: "$.forbidden[0].to.couldNotResolve", wantText: `unknown field "couldNotResolve"`},
		{name: "allowed reachable is unsupported", input: `{"allowed":[{"name":"x","from":{},"to":{"path":["a"],"reachable":true}}]}`, wantPath: "$.allowed[0].to.reachable", wantText: `unknown field "reachable"`},
		{name: "required reachable is unsupported", input: `{"required":[{"name":"x","from":{},"to":{"path":["a"],"reachable":true}}]}`, wantPath: "$.required[0].to.reachable", wantText: `unknown field "reachable"`},
		{name: "allowed moreUnstable is unsupported", input: `{"allowed":[{"name":"x","from":{},"to":{"moreUnstable":true}}]}`, wantPath: "$.allowed[0].to.moreUnstable", wantText: `unknown field "moreUnstable"`},
		{name: "required moreUnstable is unsupported", input: `{"required":[{"name":"x","from":{},"to":{"moreUnstable":true}}]}`, wantPath: "$.required[0].to.moreUnstable", wantText: `unknown field "moreUnstable"`},
		{name: "reachable must be boolean", input: validRule(`"to":{"path":["a"],"reachable":null}`), wantPath: "$.forbidden[0].to.reachable", wantText: "must be a boolean"},
		{name: "reachable false requires path", input: validRule(`"to":{"pathNot":["a"],"reachable":false}`), wantPath: "$.forbidden[0].to", wantText: `field "reachable" requires field "path"`},
		{name: "reachable rejects dependency types", input: validRule(`"to":{"path":["a"],"reachable":true,"dependencyTypes":["local"]}`), wantPath: "$.forbidden[0].to.dependencyTypes", wantText: `cannot be combined with "reachable"`},
		{name: "reachable false rejects excluded dependency types", input: validRule(`"to":{"path":["a"],"reachable":false,"dependencyTypesNot":["unresolved"]}`), wantPath: "$.forbidden[0].to.dependencyTypesNot", wantText: `cannot be combined with "reachable"`},
		{name: "reachable false rejects path capture", input: `{"forbidden":[{"name":"x","from":{"path":["^cmd/([^/]+)/"]},"to":{"path":["^internal/$1/"],"reachable":false}}]}`, wantPath: "$.forbidden[0].to.path[0]", wantText: "capture references are not allowed with reachable: false"},
		{name: "reachable false rejects pathNot capture", input: `{"forbidden":[{"name":"x","from":{"path":["^cmd/([^/]+)/"]},"to":{"path":["^internal/"],"pathNot":["^internal/$1/"],"reachable":false}}]}`, wantPath: "$.forbidden[0].to.pathNot[0]", wantText: "capture references are not allowed with reachable: false"},
		{name: "moreUnstable must be boolean", input: validRule(`"to":{"moreUnstable":null}`), wantPath: "$.forbidden[0].to.moreUnstable", wantText: "must be a boolean"},
		{name: "moreUnstable false is reserved", input: validRule(`"to":{"moreUnstable":false}`), wantPath: "$.forbidden[0].to.moreUnstable", wantText: "must be true when specified"},
		{name: "moreUnstable rejects reachable true", input: validRule(`"to":{"path":["a"],"reachable":true,"moreUnstable":true}`), wantPath: "$.forbidden[0].to.reachable", wantText: `cannot be combined with "moreUnstable"`},
		{name: "moreUnstable rejects reachable false", input: validRule(`"to":{"path":["a"],"reachable":false,"moreUnstable":true}`), wantPath: "$.forbidden[0].to.reachable", wantText: `cannot be combined with "moreUnstable"`},
		{name: "moreUnstable requires local dependency type", input: validRule(`"to":{"moreUnstable":true,"dependencyTypes":["stdlib","module"]}`), wantPath: "$.forbidden[0].to.dependencyTypes", wantText: `must include "local"`},
		{name: "moreUnstable rejects excluded local dependency type", input: validRule(`"to":{"moreUnstable":true,"dependencyTypesNot":["local"]}`), wantPath: "$.forbidden[0].to.dependencyTypesNot", wantText: `must not include "local"`},
		{name: "path must be array", input: validRule(`"from":{"path":"^cmd/"}`), wantPath: "$.forbidden[0].from.path", wantText: "must be an array"},
		{name: "path cannot be empty array", input: validRule(`"from":{"path":[]}`), wantPath: "$.forbidden[0].from.path", wantText: "at least one item"},
		{name: "path items must be strings", input: validRule(`"from":{"path":[null]}`), wantPath: "$.forbidden[0].from.path[0]", wantText: "must be a string"},
		{name: "orphan must be boolean", input: validRule(`"from":{"orphan":null}`), wantPath: "$.forbidden[0].from.orphan", wantText: "must be a boolean"},
		{name: "dependent count must be a number", input: validRule(`"from":{"numberOfDependentsMoreThan":"0"}`), wantPath: "$.forbidden[0].from.numberOfDependentsMoreThan", wantText: "must be a number"},
		{name: "negative dependent count is rejected", input: validRule(`"from":{"numberOfDependentsMoreThan":-1}`), wantPath: "$.forbidden[0].from.numberOfDependentsMoreThan", wantText: "non-negative integer"},
		{name: "fractional dependent count is rejected", input: validRule(`"from":{"numberOfDependentsLessThan":1.5}`), wantPath: "$.forbidden[0].from.numberOfDependentsLessThan", wantText: "without decimal or exponent notation"},
		{name: "exponent dependent count is rejected", input: validRule(`"from":{"numberOfDependentsMoreThan":1e2}`), wantPath: "$.forbidden[0].from.numberOfDependentsMoreThan", wantText: "without decimal or exponent notation"},
		{name: "oversized dependent count is rejected", input: validRule(`"from":{"numberOfDependentsMoreThan":999999999999999999999999}`), wantPath: "$.forbidden[0].from.numberOfDependentsMoreThan", wantText: "representable as an int"},
		{name: "less than zero is degenerate", input: validRule(`"from":{"numberOfDependentsLessThan":0}`), wantPath: "$.forbidden[0].from.numberOfDependentsLessThan", wantText: "must be at least 1"},
		{name: "equal dependent count bounds have no integer", input: validRule(`"from":{"numberOfDependentsLessThan":3,"numberOfDependentsMoreThan":3}`), wantPath: "$.forbidden[0].from.numberOfDependentsMoreThan", wantText: "empty integer range"},
		{name: "adjacent dependent count bounds have no integer", input: validRule(`"from":{"numberOfDependentsLessThan":3,"numberOfDependentsMoreThan":2}`), wantPath: "$.forbidden[0].from.numberOfDependentsMoreThan", wantText: "empty integer range"},
		{name: "invalid from path regex", input: validRule(`"from":{"path":["("]}`), wantPath: "$.forbidden[0].from.path[0]", wantText: "invalid regular expression"},
		{name: "invalid from pathNot regex", input: validRule(`"from":{"pathNot":["["]}`), wantPath: "$.forbidden[0].from.pathNot[0]", wantText: "invalid regular expression"},
		{name: "invalid packageName regex", input: validRule(`"from":{"packageName":["*"]}`), wantPath: "$.forbidden[0].from.packageName[0]", wantText: "invalid regular expression"},
		{name: "invalid to path regex", input: validRule(`"to":{"path":["("]}`), wantPath: "$.forbidden[0].to.path[0]", wantText: "invalid regular expression"},
		{name: "invalid to pathNot regex", input: validRule(`"to":{"pathNot":["["]}`), wantPath: "$.forbidden[0].to.pathNot[0]", wantText: "invalid regular expression"},
		{name: "unknown dependency type", input: validRule(`"to":{"dependencyTypes":["third-party"]}`), wantPath: "$.forbidden[0].to.dependencyTypes[0]", wantText: "unknown dependency type"},
		{name: "invalid severity", input: validRule(`"severity":"fatal"`), wantPath: "$.forbidden[0].severity", wantText: "unknown severity"},
		{name: "invalid allowed severity", input: `{"allowedSeverity":"fatal"}`, wantPath: "$.allowedSeverity", wantText: "unknown severity"},
		{name: "capture without from path", input: validRule(`"to":{"path":["^internal/$1/"]}`), wantPath: "$.forbidden[0].to.path[0]", wantText: "requires from.path"},
		{name: "required capture without from path", input: `{"required":[{"name":"x","from":{},"to":{"path":["^internal/$1/"]}}]}`, wantPath: "$.required[0].to.path[0]", wantText: "requires from.path"},
		{name: "required capture exceeds group count", input: `{"required":[{"name":"x","from":{"path":["^cmd/([^/]+)/"]},"to":{"path":["^internal/$2/"]}}]}`, wantPath: "$.required[0].to.path[0]", wantText: "exceeds the 1 groups"},
		{name: "capture exceeds group count", input: captureRule(`"^cmd/([^/]+)/"`, `"^internal/$2/"`), wantPath: "$.forbidden[0].to.path[0]", wantText: "exceeds the 1 groups"},
		{name: "every source pattern must supply capture", input: captureRule(`"^cmd/([^/]+)/","^pkg/"`, `"^internal/$1/"`), wantPath: "$.forbidden[0].to.path[0]", wantText: "from.path[1]"},
		{name: "capture zero is invalid", input: captureRule(`"^cmd/(.+)/"`, `"^internal/$0/"`), wantPath: "$.forbidden[0].to.path[0]", wantText: "references start at $1"},
		{name: "braced capture is invalid", input: captureRule(`"^cmd/(.+)/"`, `"^internal/${1}/"`), wantPath: "$.forbidden[0].to.path[0]", wantText: "braced capture references"},
		{name: "named capture reference is invalid", input: captureRule(`"^cmd/(?P<feature>.+)/"`, `"^internal/$feature/"`), wantPath: "$.forbidden[0].to.path[0]", wantText: "named capture references"},
		{name: "capture in character class is invalid", input: captureRule(`"^cmd/(.+)/"`, `"^internal/[$1]/"`), wantPath: "$.forbidden[0].to.path[0]", wantText: "character class"},
		{name: "capture after POSIX class stays in character class", input: captureRule(`"^(.*)$"`, `"[[:alpha:]$1]"`), wantPath: "$.forbidden[0].to.path[0]", wantText: "character class"},
		{name: "capture after leading literal bracket stays in character class", input: captureRule(`"^(.*)$"`, `"[^]$1]"`), wantPath: "$.forbidden[0].to.path[0]", wantText: "character class"},
		{name: "capture in quoted regex literal is invalid", input: captureRule(`"^(.*)$"`, `"^\\Q$1\\E$"`), wantPath: "$.forbidden[0].to.path[0]", wantText: "quoted literals"},
		{name: "oversized JSON number gets field path", input: `{"allowedSeverity":1e10000}`, wantPath: "$.allowedSeverity", wantText: "must be a string"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(strings.NewReader(test.input))
			if err == nil {
				t.Fatal("Load() error = nil")
			}
			var configErr *Error
			if !errors.As(err, &configErr) {
				t.Fatalf("Load() error type = %T, want *config.Error: %v", err, err)
			}
			if configErr.Source != anonymousSource || configErr.Offset < 1 || configErr.Line < 1 || configErr.Column < 1 {
				t.Errorf("position = %#v, want populated one-based anonymous position", configErr)
			}
			if configErr.Path != test.wantPath {
				t.Errorf("Path = %q, want %q", configErr.Path, test.wantPath)
			}
			if !strings.Contains(err.Error(), test.wantText) {
				t.Errorf("error = %q, want it to contain %q", err, test.wantText)
			}
		})
	}
}

func TestLoadReportsExactRegexPosition(t *testing.T) {
	t.Parallel()

	input := `{
  "forbidden": [{
    "name": "bad-regex",
    "from": {
      "path": ["("]
    },
    "to": {}
  }]
}`
	_, err := Parse([]byte(input))
	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatalf("Parse() error = %v, want *config.Error", err)
	}
	if configErr.Line != 5 || configErr.Column != 16 {
		t.Errorf("position = %d:%d, want 5:16", configErr.Line, configErr.Column)
	}
}

func TestLoadReportsExactDependentCountPosition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fromFields string
		wantPath   string
		wantLine   int
	}{
		{
			name:       "negative value",
			fromFields: `      "numberOfDependentsMoreThan": -1`,
			wantPath:   "$.forbidden[0].from.numberOfDependentsMoreThan",
			wantLine:   5,
		},
		{
			name:       "fractional value",
			fromFields: `      "numberOfDependentsLessThan": 1.5`,
			wantPath:   "$.forbidden[0].from.numberOfDependentsLessThan",
			wantLine:   5,
		},
		{
			name: "empty integer range",
			fromFields: "      \"numberOfDependentsLessThan\": 3,\n" +
				`      "numberOfDependentsMoreThan": 2`,
			wantPath: "$.forbidden[0].from.numberOfDependentsMoreThan",
			wantLine: 6,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			input := "{\n" +
				"  \"forbidden\": [{\n" +
				"    \"name\": \"invalid-count\",\n" +
				"    \"from\": {\n" +
				test.fromFields + "\n" +
				"    },\n" +
				"    \"to\": {}\n" +
				"  }]\n" +
				"}"
			_, err := Parse([]byte(input))
			var configErr *Error
			if !errors.As(err, &configErr) {
				t.Fatalf("Parse() error = %v, want *config.Error", err)
			}
			if configErr.Path != test.wantPath || configErr.Line != test.wantLine || configErr.Column != 37 {
				t.Errorf(
					"position = %s %d:%d, want %s %d:37",
					configErr.Path,
					configErr.Line,
					configErr.Column,
					test.wantPath,
					test.wantLine,
				)
			}
		})
	}
}

func TestLoadReportsExactRequiredCapturePosition(t *testing.T) {
	t.Parallel()

	input := `{
  "required": [{
    "name": "needs-capture",
    "from": {},
    "to": {
      "path": ["^internal/$1/"]
    }
  }]
}`
	_, err := Parse([]byte(input))
	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatalf("Parse() error = %v, want *config.Error", err)
	}
	if configErr.Path != "$.required[0].to.path[0]" ||
		configErr.Line != 6 || configErr.Column != 16 {
		t.Errorf(
			"required capture error = path %q at %d:%d, want $.required[0].to.path[0] at 6:16",
			configErr.Path,
			configErr.Line,
			configErr.Column,
		)
	}
}

func TestLoadFileIncludesFilename(t *testing.T) {
	t.Parallel()

	filename := filepath.Join(t.TempDir(), "godep-cruiser.json")
	if err := os.WriteFile(filename, []byte(`{"unknown":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := LoadFile(filename)
	var configErr *Error
	if !errors.As(err, &configErr) {
		t.Fatalf("LoadFile() error = %v, want *config.Error", err)
	}
	if configErr.Source != filename {
		t.Errorf("Source = %q, want %q", configErr.Source, filename)
	}
}

func TestExpandCaptures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pattern  string
		captures []string
		want     string
		wantErr  string
	}{
		{
			name:     "quotes captured path text",
			pattern:  `^internal/$1/$2$`,
			captures: []string{"whole", "feature.name", "sub+pkg"},
			want:     `^internal/(?:feature\.name)/(?:sub\+pkg)$`,
		},
		{
			name:     "keeps escaped dollar literal",
			pattern:  `^literal/\$1$`,
			captures: []string{"whole"},
			want:     `^literal/\$1$`,
		},
		{
			name:     "expands an unmatched optional group as empty",
			pattern:  `^internal/$1/end$`,
			captures: []string{"whole", ""},
			want:     `^internal/(?:)/end$`,
		},
		{
			name:     "keeps an empty capture valid before a quantifier",
			pattern:  `^$1*$`,
			captures: []string{"whole", ""},
			want:     `^(?:)*$`,
		},
		{
			name:     "rejects unavailable group",
			pattern:  `$2`,
			captures: []string{"whole", "one"},
			wantErr:  "unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExpandCaptures(test.pattern, test.captures)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("ExpandCaptures() error = %v, want %q", err, test.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ExpandCaptures() error = %v", err)
			}
			if got != test.want {
				t.Errorf("ExpandCaptures() = %q, want %q", got, test.want)
			}
			if _, err := regexp.Compile(got); err != nil {
				t.Errorf("expanded regular expression %q does not compile: %v", got, err)
			}
		})
	}
}

func TestConfigJSONRoundTripPreservesEmptyAllowed(t *testing.T) {
	t.Parallel()

	loaded, err := Parse([]byte(`{"allowed":[]}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	data, err := json.Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"allowed":[]`) {
		t.Fatalf("Marshal() = %s, want an explicit empty allowed array", data)
	}
	reloaded, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse(round trip) error = %v", err)
	}
	if reloaded.Allowed == nil || len(reloaded.Allowed) != 0 {
		t.Errorf("Allowed after round trip = %#v, want non-nil empty slice", reloaded.Allowed)
	}
}

func TestExpandCapturesCannotCreateCountedRepetition(t *testing.T) {
	t.Parallel()

	expanded, err := ExpandCaptures(`^x{$1}$`, []string{"whole", "2"})
	if err != nil {
		t.Fatalf("ExpandCaptures() error = %v", err)
	}
	pattern, err := regexp.Compile(expanded)
	if err != nil {
		t.Fatalf("Compile(%q) error = %v", expanded, err)
	}
	if !pattern.MatchString(`x{2}`) || pattern.MatchString("xx") {
		t.Errorf("expanded pattern %q must match literal x{2}, not a repeated x", expanded)
	}
}

func TestPublishedSchemaCoversConfigFields(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "schema", "godep-cruiser.schema.json"))
	if err != nil {
		t.Fatalf("ReadFile(schema) error = %v", err)
	}
	var schema struct {
		Dialect     string                     `json:"$schema"`
		Properties  map[string]json.RawMessage `json:"properties"`
		Definitions map[string]struct {
			Properties       map[string]json.RawMessage `json:"properties"`
			DependentSchemas map[string]json.RawMessage `json:"dependentSchemas"`
			AllOf            []json.RawMessage          `json:"allOf"`
			Enum             []string                   `json:"enum"`
			Type             string                     `json:"type"`
			MinLength        int                        `json:"minLength"`
			Not              struct {
				Const string `json:"const"`
			} `json:"not"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("schema dialect = %q", schema.Dialect)
	}

	tests := []struct {
		name       string
		properties map[string]json.RawMessage
		want       []string
	}{
		{name: "configuration", properties: schema.Properties, want: []string{"forbidden", "required", "allowed", "allowedSeverity"}},
		{name: "from", properties: schema.Definitions["from"].Properties, want: []string{"path", "pathNot", "orphan", "packageName", "numberOfDependentsLessThan", "numberOfDependentsMoreThan"}},
		{name: "required from", properties: schema.Definitions["requiredFrom"].Properties, want: []string{"path", "pathNot", "packageName"}},
		{name: "to", properties: schema.Definitions["to"].Properties, want: []string{"path", "pathNot", "dependencyTypes", "dependencyTypesNot"}},
		{name: "forbidden to", properties: schema.Definitions["forbiddenTo"].Properties, want: []string{"path", "pathNot", "reachable", "moreUnstable", "dependencyTypes", "dependencyTypesNot"}},
		{name: "forbidden rule", properties: schema.Definitions["forbiddenRule"].Properties, want: []string{"name", "comment", "severity", "scope", "from", "to"}},
		{name: "required rule", properties: schema.Definitions["requiredRule"].Properties, want: []string{"name", "comment", "severity", "from", "to"}},
		{name: "allowed rule", properties: schema.Definitions["allowedRule"].Properties, want: []string{"name", "comment", "from", "to"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if len(test.properties) != len(test.want) {
				t.Errorf("property count = %d, want %d", len(test.properties), len(test.want))
			}
			for _, name := range test.want {
				if _, ok := test.properties[name]; !ok {
					t.Errorf("schema is missing property %q", name)
				}
			}
		})
	}

	enumTests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "severity",
			got:  schema.Definitions["severity"].Enum,
			want: []string{"error", "warn", "info", "ignore"},
		},
		{
			name: "dependency type",
			got:  schema.Definitions["dependencyType"].Enum,
			want: []string{"stdlib", "local", "module", "unresolved"},
		},
		{
			name: "scope",
			got:  schema.Definitions["scope"].Enum,
			want: []string{"module", "folder"},
		},
	}
	for _, test := range enumTests {
		t.Run(test.name+" enum", func(t *testing.T) {
			t.Parallel()
			if strings.Join(test.got, ",") != strings.Join(test.want, ",") {
				t.Errorf("enum = %q, want %q", test.got, test.want)
			}
		})
	}

	ruleName := schema.Definitions["ruleName"]
	if ruleName.Type != "string" || ruleName.MinLength != 1 || ruleName.Not.Const != "not-in-allowed" {
		t.Errorf("ruleName schema = %#v, want a non-empty string excluding not-in-allowed", ruleName)
	}
	for _, definition := range []string{"forbiddenRule", "requiredRule", "allowedRule"} {
		var nameProperty struct {
			Reference string `json:"$ref"`
		}
		if err := json.Unmarshal(schema.Definitions[definition].Properties["name"], &nameProperty); err != nil {
			t.Fatalf("decode %s name property: %v", definition, err)
		}
		if nameProperty.Reference != "#/$defs/ruleName" {
			t.Errorf("%s name reference = %q, want #/$defs/ruleName", definition, nameProperty.Reference)
		}
	}

	var scopeProperty struct {
		Reference string `json:"$ref"`
		Default   string `json:"default"`
	}
	if err := json.Unmarshal(schema.Definitions["forbiddenRule"].Properties["scope"], &scopeProperty); err != nil {
		t.Fatalf("decode forbiddenRule scope property: %v", err)
	}
	if scopeProperty.Reference != "#/$defs/scope" || scopeProperty.Default != "module" {
		t.Errorf("forbiddenRule scope schema = %#v, want #/$defs/scope defaulting to module", scopeProperty)
	}

	if len(schema.Definitions["forbiddenRule"].AllOf) != 1 {
		t.Fatalf("forbiddenRule allOf count = %d, want 1", len(schema.Definitions["forbiddenRule"].AllOf))
	}
	var folderScopeCondition struct {
		If struct {
			Properties map[string]struct {
				Const string `json:"const"`
			} `json:"properties"`
			Required []string `json:"required"`
		} `json:"if"`
		Then struct {
			Properties map[string]struct {
				Not struct {
					AnyOf []struct {
						Required []string `json:"required"`
					} `json:"anyOf"`
				} `json:"not"`
			} `json:"properties"`
		} `json:"then"`
	}
	if err := json.Unmarshal(schema.Definitions["forbiddenRule"].AllOf[0], &folderScopeCondition); err != nil {
		t.Fatalf("decode forbiddenRule folder scope condition: %v", err)
	}
	if folderScopeCondition.If.Properties["scope"].Const != "folder" ||
		strings.Join(folderScopeCondition.If.Required, ",") != "scope" {
		t.Errorf("forbiddenRule folder condition = %#v, want explicit scope folder", folderScopeCondition.If)
	}
	folderConflicts := func(property string) map[string]bool {
		result := make(map[string]bool)
		for _, condition := range folderScopeCondition.Then.Properties[property].Not.AnyOf {
			if len(condition.Required) == 1 {
				result[condition.Required[0]] = true
			}
		}

		return result
	}
	fromConflicts := folderConflicts("from")
	if len(fromConflicts) != 2 || !fromConflicts["orphan"] || !fromConflicts["packageName"] {
		t.Errorf("folder from conflicts = %v, want orphan and packageName", fromConflicts)
	}
	toConflicts := folderConflicts("to")
	if len(toConflicts) != 3 || !toConflicts["reachable"] ||
		!toConflicts["dependencyTypes"] || !toConflicts["dependencyTypesNot"] {
		t.Errorf(
			"folder to conflicts = %v, want reachable, dependencyTypes, and dependencyTypesNot",
			toConflicts,
		)
	}
	dependentCountTests := []struct {
		name    string
		minimum int
	}{
		{name: "numberOfDependentsLessThan", minimum: 1},
		{name: "numberOfDependentsMoreThan", minimum: 0},
	}
	for _, test := range dependentCountTests {
		t.Run(test.name+" schema", func(t *testing.T) {
			t.Parallel()

			var property struct {
				Type    string `json:"type"`
				Minimum *int   `json:"minimum"`
			}
			if err := json.Unmarshal(schema.Definitions["from"].Properties[test.name], &property); err != nil {
				t.Fatalf("decode from.%s: %v", test.name, err)
			}
			if property.Type != "integer" || property.Minimum == nil || *property.Minimum != test.minimum {
				t.Errorf("from.%s schema = %#v, want integer minimum %d", test.name, property, test.minimum)
			}
		})
	}

	var reachableProperty struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(schema.Definitions["forbiddenTo"].Properties["reachable"], &reachableProperty); err != nil {
		t.Fatalf("decode forbiddenTo reachable property: %v", err)
	}
	if reachableProperty.Type != "boolean" {
		t.Errorf("forbiddenTo reachable type = %q, want boolean", reachableProperty.Type)
	}
	var moreUnstableProperty struct {
		Type  string `json:"type"`
		Const *bool  `json:"const"`
	}
	if err := json.Unmarshal(schema.Definitions["forbiddenTo"].Properties["moreUnstable"], &moreUnstableProperty); err != nil {
		t.Fatalf("decode forbiddenTo moreUnstable property: %v", err)
	}
	if moreUnstableProperty.Type != "boolean" || moreUnstableProperty.Const == nil || !*moreUnstableProperty.Const {
		t.Errorf("forbiddenTo moreUnstable schema = %#v, want boolean const true", moreUnstableProperty)
	}

	ruleToTests := []struct {
		definition string
		want       string
	}{
		{definition: "forbiddenRule", want: "#/$defs/forbiddenTo"},
		{definition: "allowedRule", want: "#/$defs/to"},
	}
	for _, test := range ruleToTests {
		var property struct {
			Reference string `json:"$ref"`
		}
		if err := json.Unmarshal(schema.Definitions[test.definition].Properties["to"], &property); err != nil {
			t.Fatalf("decode %s to property: %v", test.definition, err)
		}
		if property.Reference != test.want {
			t.Errorf("%s to reference = %q, want %q", test.definition, property.Reference, test.want)
		}
	}

	var reachableDependencies struct {
		Required []string `json:"required"`
		Not      struct {
			AnyOf []struct {
				Required []string `json:"required"`
			} `json:"anyOf"`
		} `json:"not"`
	}
	if err := json.Unmarshal(
		schema.Definitions["forbiddenTo"].DependentSchemas["reachable"],
		&reachableDependencies,
	); err != nil {
		t.Fatalf("decode forbiddenTo reachable dependencies: %v", err)
	}
	if strings.Join(reachableDependencies.Required, ",") != "path" {
		t.Errorf("reachable required fields = %q, want path", reachableDependencies.Required)
	}
	conflicts := make(map[string]bool)
	for _, condition := range reachableDependencies.Not.AnyOf {
		if len(condition.Required) == 1 {
			conflicts[condition.Required[0]] = true
		}
	}
	if len(conflicts) != 3 || !conflicts["moreUnstable"] || !conflicts["dependencyTypes"] || !conflicts["dependencyTypesNot"] {
		t.Errorf(
			"reachable conflicting fields = %v, want moreUnstable, dependencyTypes, and dependencyTypesNot",
			conflicts,
		)
	}

	var moreUnstableDependencies struct {
		Not struct {
			Required []string `json:"required"`
		} `json:"not"`
		Properties map[string]struct {
			Contains struct {
				Const string `json:"const"`
			} `json:"contains"`
			Not struct {
				Contains struct {
					Const string `json:"const"`
				} `json:"contains"`
			} `json:"not"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(
		schema.Definitions["forbiddenTo"].DependentSchemas["moreUnstable"],
		&moreUnstableDependencies,
	); err != nil {
		t.Fatalf("decode forbiddenTo moreUnstable dependencies: %v", err)
	}
	if strings.Join(moreUnstableDependencies.Not.Required, ",") != "reachable" {
		t.Errorf("moreUnstable conflicting fields = %q, want reachable", moreUnstableDependencies.Not.Required)
	}
	if got := moreUnstableDependencies.Properties["dependencyTypes"].Contains.Const; got != "local" {
		t.Errorf("moreUnstable dependencyTypes contains = %q, want local", got)
	}
	if got := moreUnstableDependencies.Properties["dependencyTypesNot"].Not.Contains.Const; got != "local" {
		t.Errorf("moreUnstable dependencyTypesNot exclusion = %q, want local", got)
	}

	var requiredTo struct {
		Reference     string `json:"$ref"`
		MinProperties int    `json:"minProperties"`
	}
	if err := json.Unmarshal(schema.Definitions["requiredRule"].Properties["to"], &requiredTo); err != nil {
		t.Fatalf("decode requiredRule to property: %v", err)
	}
	if requiredTo.Reference != "#/$defs/to" || requiredTo.MinProperties != 1 {
		t.Errorf("requiredRule to schema = %#v, want non-empty #/$defs/to", requiredTo)
	}
}

func validRule(replacement string) string {
	fields := []string{`"name":"x"`, `"from":{}`, `"to":{}`}
	key := strings.SplitN(replacement, ":", 2)[0]
	for index, field := range fields {
		if strings.HasPrefix(field, key+":") {
			fields[index] = replacement

			return `{"forbidden":[{` + strings.Join(fields, ",") + `}]}`
		}
	}
	fields = append(fields, replacement)

	return `{"forbidden":[{` + strings.Join(fields, ",") + `}]}`
}

func captureRule(fromPatterns, toPattern string) string {
	return `{"forbidden":[{"name":"x","from":{"path":[` + fromPatterns + `]},"to":{"path":[` + toPattern + `]}}]}`
}
