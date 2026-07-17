# godep-cruiser

Validate dependency rules for Go source trees.

godep-cruiser is a clean-room Go reimplementation of the concepts in
[dependency-cruiser](https://github.com/sverweij/dependency-cruiser) by
Sander Verweij — forbidden/allowed/required rules with regex `path` / `pathNot`
matching, transitive reachability, package fan-in, and instability checks,
dependency-type classification (stdlib / in-module / third-party / unresolved),
and a violation baseline.
It adds one thing the original does not have: stale baseline entries fail
the run, so grandfathered exceptions expire automatically when the
violation they cover disappears.

No code is translated from dependency-cruiser; the design derives from its
public documentation and observable behavior. Not affiliated with the
upstream project.

## Quick start

Install the command from the module root:

```sh
go install github.com/butaosuinu/godep-cruiser@latest
```

Save a rule configuration as `godep-cruiser.json` (the complete example in
[Configuration](#configuration) is valid), then validate the current module:

```sh
godep-cruiser --config godep-cruiser.json --scan-root .
```

Human-readable `err` output is the default. JSON, Mermaid, and GraphViz DOT
are selected explicitly:

```sh
godep-cruiser --config godep-cruiser.json --scan-root . --output-type json
godep-cruiser --config godep-cruiser.json --scan-root . --output-type mermaid
godep-cruiser --config godep-cruiser.json --scan-root . --output-type dot
```

Mermaid and DOT visualize only the violation-induced subgraph because a
validation result does not contain non-violating dependencies. Edge violations
are labeled edges, source-only violations are attached to highlighted source
nodes, and stale baseline entries are independent highlighted nodes.

In JSON reports, `violations[].kind` is one of:

- `forbidden` for a forbidden-rule match, including folder-scoped package
  edges and source-only orphan, package-name, and dependent-count checks
- `not-in-allowed` for a dependency that matches no allowed rule
- `required` for a source file missing a required import
- `reachable` for a matching package that is transitively reachable
- `unreachable` for a matching package outside the entry-point closure

Source-only violations serialize `to` as `null`; edge violations include the
target dependency or package.

Generate and then apply an exact-match baseline:

```sh
godep-cruiser --config godep-cruiser.json --scan-root . \
  --generate-baseline > godep-cruiser-baseline.json

godep-cruiser --config godep-cruiser.json --scan-root . \
  --baseline godep-cruiser-baseline.json
```

Validation exits with the number of unsuppressed `error` violations plus stale
baseline entries, capped at 255 so every failing validation stays non-zero as a
process status. Warnings and informational violations are still reported but do
not make the command fail. Flag, configuration, scan, and output failures exit
2; successful baseline generation exits 0.

## Why

Go's compiler forbids import cycles but says nothing about architecture:
layer direction, stdlib purity of a core package, or a tools tree that
must stay dependency-free. Existing Go tools each miss part of that space
(no stdlib restriction, no file-level exceptions, no fail-closed
classification, no self-expiring exceptions). godep-cruiser targets that
gap with a rules model proven by dependency-cruiser.

## Configuration

v0.3 configuration is JSON-only so the runtime remains standard-library-only.
The published [JSON Schema](schema/godep-cruiser.schema.json) describes every
accepted field; the loader also validates Go regular expressions, numeric
capture references, unknown fields, and source positions.

```json
{
  "forbidden": [
    {
      "name": "features-stay-independent",
      "severity": "error",
      "from": {
        "path": ["^internal/features/([^/]+)/"]
      },
      "to": {
        "path": ["^internal/features/"],
        "pathNot": ["^internal/features/$1/"],
        "dependencyTypes": ["local"]
      }
    },
    {
      "name": "entrypoints-reach-production",
      "severity": "error",
      "from": {
        "path": ["^cmd/"]
      },
      "to": {
        "path": ["^internal/"],
        "pathNot": ["^internal/testutil(/|$)"],
        "reachable": false,
        "reachableFilePathNot": ["_test\\.go$"]
      }
    },
    {
      "name": "shared-packages-have-multiple-dependents",
      "severity": "warn",
      "from": {
        "path": ["^internal/shared/"],
        "numberOfDependentsLessThan": 2
      },
      "to": {}
    },
    {
      "name": "core-packages-do-not-import-adapters",
      "severity": "error",
      "scope": "folder",
      "from": {
        "path": ["^internal/core(?:/|$)"]
      },
      "to": {
        "path": ["^internal/adapters(?:/|$)"]
      }
    }
  ],
  "required": [
    {
      "name": "services-require-logging",
      "severity": "error",
      "from": {
        "path": ["^internal/services/"]
      },
      "to": {
        "path": ["^internal/logging$"],
        "dependencyTypes": ["local"]
      }
    }
  ],
  "allowed": [
    {
      "name": "allow-resolved-dependencies",
      "from": {},
      "to": {
        "dependencyTypes": ["stdlib", "local", "module"]
      }
    }
  ],
  "allowedSeverity": "error"
}
```

`from.path` capture groups can be referenced as `$1`, `$2`, and later numeric
references in `to.path` and `to.pathNot`. See
[DESIGN.ja.md](DESIGN.ja.md#設定形式と-loader) for the matching and validation
semantics.

A forbidden rule's optional `scope` is `module` by default. The default scope
matches individual source files and imports. `scope: "folder"` instead matches
`from.path` and `to.path` against module-relative package paths (`.` is the
module root) and reports one violation per distinct local package edge. A
folder-scoped violation has the source package path in `from.path`, line `0`,
an empty package name, and a local target package with no raw import path.
Folder scope is available only to forbidden rules. It rejects
`dependencyTypes`, `dependencyTypesNot`, `from.orphan`, `from.packageName`, and
`to.reachable`; dependent-count conditions and `from.path` capture references
in `to` remain available.

Each `required` rule checks every file matching `from` and reports one
source-only violation when none of that file's imports matches `to`. An
importless matching file therefore violates the rule. `from: {}` is a
catch-all; `to: {}` and `from.orphan` are invalid for required rules.

A forbidden rule can set `to.reachable` to evaluate the local package graph.
`true` reports each matching target package reachable from a matching file's
local imports; the diagnostic line identifies the import that starts the path.
`false` treats packages containing matching files as entry points and reports
every file in a matching target package outside their transitive closure. Both
forms require `to.path`, allow `to.pathNot`, and reject dependency-type fields.
Capture references remain available for `true` but are invalid for `false`;
allowed and required rules do not accept `reachable`.

`to.reachableFilePathNot` optionally excludes transitive local-package edges by
the scan-root-relative, slash-separated paths of the files that form them. It
is a non-empty regular-expression array available only with `to.reachable`, and
it does not expand `from.path` captures. The field is opt-in: omitting it keeps
all edges, including edges formed only by `_test.go` files. If both production
and excluded files form the same package edge, the edge remains traversable;
the filter removes an edge only when every file forming it is excluded.

For `reachable: true`, the filter starts after each matching source file's
initiating local import, so `from.pathNot` remains the way to exclude seed
files. For `reachable: false`, it applies to every edge followed from the seed
packages. Filtering changes closure membership only; violation shapes and
baseline identities remain unchanged.

`from.numberOfDependentsLessThan` and
`from.numberOfDependentsMoreThan` compare the source package's distinct direct
local dependents with strict `<` and `>` bounds. In a module-scoped forbidden
rule, combining either condition with `to: {}` reports one source-only
violation per matching file. In folder scope, `to: {}` matches every outgoing
local package edge, so the condition filters source packages and an importless
package produces no violation. Required rules do not accept dependent-count
conditions.

A forbidden rule can set `to.moreUnstable: true` to match a local edge only
when the target package is strictly more unstable than the source package.
Package instability is `FanOut / (FanIn + FanOut)` over distinct local package
edges with self-edges excluded; a zero denominator is defined as `0`. Equal
values do not match, and stdlib, third-party, and unresolved dependencies never
match. The field is available in module and folder scope, but `false`, allowed
or required rules, `reachable`, a `dependencyTypes` list without `local`, and a
`dependencyTypesNot` list containing `local` are rejected.

## Library API

The public facade is importable independently of the CLI. Configuration load,
scan, validation, optional baseline filtering, reporting, and error counting
remain ordinary Go calls:

```go
configuration, err := config.LoadFile("godep-cruiser.json")
if err != nil {
	return err
}
result, err := cruiser.Validate(configuration, cruiser.Options{ScanRoot: "."})
if err != nil {
	return err
}
if err := cruiser.WriteReport(os.Stdout, cruiser.OutputTypeErr, result); err != nil {
	return err
}
if result.ErrorCount() != 0 {
	return fmt.Errorf("dependency validation failed with %d errors", result.ErrorCount())
}
```

Import `github.com/butaosuinu/godep-cruiser/config` and
`github.com/butaosuinu/godep-cruiser/cruiser` for the snippet above. Set
`Options.GoModPath` when the module file is not `<ScanRoot>/go.mod`; no
`go.work` or nested-module discovery is performed.

### Go test helper

Dependency validation can run as an ordinary Go test. Place a test like this in
the module root:

```go
func TestArchitecture(t *testing.T) {
	configuration, err := config.LoadFile("godep-cruiser.json")
	if err != nil {
		t.Fatal(err)
	}

	archtest.Check(t, configuration, cruiser.Options{ScanRoot: "."})
}
```

Import `github.com/butaosuinu/godep-cruiser/archtest` in addition to `config`
and `cruiser`. Error-severity violations and stale baseline entries fail the
test. A run containing only warning or informational violations is logged
without failing it; configuration and scan errors stop the test with `Fatalf`.

## Baseline

A baseline is a strict JSON document containing exact violation keys:

```json
{
  "entries": [
    {
      "rule": "features-stay-independent",
      "from": "internal/features/orders/service.go",
      "to": "example.com/project/internal/features/payments"
    },
    {
      "rule": "no-orphans",
      "from": "internal/legacy/unused.go"
    }
  ]
}
```

For an ordinary import edge, the key is `rule` + `from` + `to`, where `to` is
the raw import path written in the Go source rather than a resolved path. A
`reachable: true` violation has no single raw target import, so its `to` key is
the module-relative target package path. A folder-scoped package edge likewise
uses module-relative package paths for both `from` and `to`. Source-only
violations such as module-scoped orphan, package-name, and dependent-count
checks, required rules, and `reachable: false` rules omit `to` and match on the
pair `rule` and `from`.

The baseline has three outcomes:

- An unlisted current violation is reported with its configured severity; a
  baseline never upgrades it.
- A matching current violation is known and suppressed.
- An entry with no matching current violation is always a stale error whose
  diagnostic tells the user to remove the entry from the baseline.

Generated entries are sorted and deduplicated. Loading rejects unknown fields,
empty keys, duplicate keys, and trailing JSON, but does not require referenced
files or imports to still exist because stale entries may point to deleted
source. Regex entries, `//nolint` directives, and date-based expiry are not
supported; exact keys make stale detection deterministic, and entries expire
when their violations disappear.

## License

[MIT](LICENSE)
