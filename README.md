# godep-cruiser

Validate dependency rules for Go source trees.

godep-cruiser is a clean-room Go reimplementation of the concepts in
[dependency-cruiser](https://github.com/sverweij/dependency-cruiser) by
Sander Verweij — forbidden/allowed rules with regex `path` / `pathNot`
matching at file granularity, dependency-type classification
(stdlib / in-module / third-party / unresolved), and a violation baseline.
It adds one thing the original does not have: stale baseline entries fail
the run, so grandfathered exceptions expire automatically when the
violation they cover disappears.

No code is translated from dependency-cruiser; the design derives from its
public documentation and observable behavior. Not affiliated with the
upstream project.

## Status

Design phase. See [DESIGN.ja.md](DESIGN.ja.md) (Japanese) for the design
document and the v0.1 scope. Implementation is tracked in the issues.

## Why

Go's compiler forbids import cycles but says nothing about architecture:
layer direction, stdlib purity of a core package, or a tools tree that
must stay dependency-free. Existing Go tools each miss part of that space
(no stdlib restriction, no file-level exceptions, no fail-closed
classification, no self-expiring exceptions). godep-cruiser targets that
gap with a rules model proven by dependency-cruiser.

## Configuration

v0.1 configuration is JSON-only so the runtime remains standard-library-only.
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
violations such as orphan and package-name rules omit `to` and match on the
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
