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
				if got.AllowedSeverity != SeverityWarn {
					t.Errorf("default AllowedSeverity = %q, want %q", got.AllowedSeverity, SeverityWarn)
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
		{name: "unknown rule field", input: validRule(`"scope":"module"`), wantPath: "$.forbidden[0].scope", wantText: `unknown field "scope"`},
		{name: "allowed rule severity is unsupported", input: `{"allowed":[{"name":"x","severity":"error","from":{},"to":{}}]}`, wantPath: "$.allowed[0].severity", wantText: `unknown field "severity"`},
		{name: "required to must define a condition", input: `{"required":[{"name":"x","from":{},"to":{}}]}`, wantPath: "$.required[0].to", wantText: "must define at least one condition"},
		{name: "required from orphan is unsupported", input: `{"required":[{"name":"x","from":{"orphan":false},"to":{"path":["a"]}}]}`, wantPath: "$.required[0].from.orphan", wantText: `unknown field "orphan"`},
		{name: "required from dependent count is unsupported", input: `{"required":[{"name":"x","from":{"numberOfDependentsMoreThan":0},"to":{"path":["a"]}}]}`, wantPath: "$.required[0].from.numberOfDependentsMoreThan", wantText: `unknown field "numberOfDependentsMoreThan"`},
		{name: "unknown from field", input: validRule(`"from":{"reachable":true}`), wantPath: "$.forbidden[0].from.reachable", wantText: `unknown field "reachable"`},
		{name: "unknown to field", input: validRule(`"to":{"couldNotResolve":true}`), wantPath: "$.forbidden[0].to.couldNotResolve", wantText: `unknown field "couldNotResolve"`},
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
			Properties map[string]json.RawMessage `json:"properties"`
			Enum       []string                   `json:"enum"`
			Type       string                     `json:"type"`
			MinLength  int                        `json:"minLength"`
			Not        struct {
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
		{name: "forbidden rule", properties: schema.Definitions["forbiddenRule"].Properties, want: []string{"name", "comment", "severity", "from", "to"}},
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
