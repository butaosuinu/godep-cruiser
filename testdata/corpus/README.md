# Violation corpus

Each child directory is a standalone Go module containing a deliberately
invalid dependency graph. The Go itself remains valid: the corpus harness
parses every scanner-visible `.go` file with `go/parser` and runs
`go vet ./...` in every module.

Scan each child module separately. Scanning `testdata/corpus` as one root is
not supported, and the repository scanner intentionally skips `testdata`
while scanning the main module.

## Golden format

Every module has a `violations.golden.json` file. The `name` follows
`<rule family>: <expected behavior>` and is used directly as the table-driven
test name. It must identify the rule, the condition, and the expected outcome
without relying on the directory name.

Each expected violation contains:

- `rule` and `severity` (`error`, `warn`, `info`, or `ignore`)
- `from.path`, relative to the fixture module with `/` separators
- `from.line`, the import line for edge violations or package line for
  source-only violations
- optional `to.path` and `to.dependencyType`; omit `to` for source-only rules
  such as orphan and package-name checks

Dependency classification is delegated to `internal/scanner`. `to.path` uses
the resolver's normalized path when it is non-empty and otherwise retains the
source import path. In particular, scanner keeps the cgo pseudo-import as raw
path `C`, empty resolved path, and type `unresolved`; the golden target is
therefore `C`. Violation arrays are sorted by rule, severity, from path, line,
to path, and dependency type. The loader rejects unknown fields, duplicate JSON
object keys, invalid enum values, stale source locations, and repeated
violation identities even when their severities differ.

Optional `positiveControls` pin source facts that must stay in a fixture but
must not appear in engine violation output. Each control has `rule` and `from`,
plus exactly one of an import target in `to` or a source `packageName`. Controls
are sorted and checked as strictly as violations. They keep allowed imports,
exceptions, and allowed package roots from disappearing while a
violation-only comparison still passes. A control and violation may not claim
the same rule/source/target identity.

Baseline inputs and their stale-entry diagnostics are separate from this live
violation golden. Issue #6 owns those artifacts and the stale corpus case, so
they will be added together once the baseline output contract exists.

The corpus deliberately does not include rule configuration files. Issue #3
owns that format. Engine tests can construct rules through the eventual Go API,
run each module as its scan root, project results onto this stable structure,
and compare them with the golden list.

## Cases

| Directory | Case pinned by the module |
|---|---|
| `layer-direction` | Core may import core and a pinned migration file may import infra, but another core-to-infra edge is rejected. |
| `stdlib-denylist-exception` | Exact stdlib bans honor a package/import exception without exempting sibling imports. |
| `third-party-in-core` | Core rejects a third-party module dependency. |
| `stdlib-only-tree` | A tools tree may use stdlib but rejects a local dependency. |
| `forbidden-import-target` | Product code rejects imports of a designated entrypoint tree. |
| `orphan-file` | A disconnected file is reported while connected files are not. |
| `package-main-placement` | `package main` is rejected outside approved command and tool roots. |
| `unclassified-dependency` | An allowed-rule set fails closed on an unclassified local dependency. |

These are the eight semantic cases owned by issue #4. They are inspired by
fanout's architecture checks but are not a one-to-one copy of its test
functions; filesystem tree-shape checks remain outside the import graph's
scope.
