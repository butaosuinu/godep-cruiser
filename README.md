# godep-cruiser

Validate dependency rules for Go source trees.

godep-cruiser is a clean-room Go reimplementation of the concepts in
[dependency-cruiser](https://github.com/sverweij/dependency-cruiser) by
Sander Verweij — forbidden/allowed/required rules with regex `path` / `pathNot`
matching at file granularity, dependency-type classification
(stdlib / in-module / third-party / unresolved), and a violation baseline.
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

Human-readable `err` output is the default. JSON and Mermaid are selected
explicitly:

```sh
godep-cruiser --config godep-cruiser.json --scan-root . --output-type json
godep-cruiser --config godep-cruiser.json --scan-root . --output-type mermaid
```

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

v0.2 configuration is JSON-only so the runtime remains standard-library-only.
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

Each `required` rule checks every file matching `from` and reports one
source-only violation when none of that file's imports matches `to`. An
importless matching file therefore violates the rule. `from: {}` is a
catch-all; `to: {}` and `from.orphan` are invalid for required rules.

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

For an import edge, the key is `rule` + `from` + `to`, where `to` is the raw
import path written in the Go source rather than a resolved path. Source-only
violations such as orphan, package-name, and required rules omit `to` and match
on the pair `rule` and `from`.

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
