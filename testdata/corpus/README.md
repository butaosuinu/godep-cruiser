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
- `from.line`, the direct import line for ordinary edge violations, the
  initiating local import line for reachable violations, or the package line
  for source-only violations
- optional `to.path` and `to.dependencyType`; `package-main-placement`,
  `no-orphans`, `handler-requires-logging`, `minimum-two-dependents`,
  `maximum-two-dependents`, and `entrypoint-reaches-production` violations are
  source-only and must omit `to`, while every other corpus violation must
  include it

Dependency classification is delegated to `internal/scanner`. `to.path` uses
the resolver's normalized path when it is non-empty and otherwise retains the
source import path. In particular, scanner keeps the cgo pseudo-import as raw
path `C`, empty resolved path, and type `unresolved`; the golden target is
therefore `C`. Reachable violations are the exception: their target is the
module-relative package reached through the local graph rather than the import
named on `from.line`. Violation arrays are sorted by rule, severity, from path,
line, to path, and dependency type. The loader rejects unknown fields,
duplicate JSON object keys, invalid enum values, stale source locations, and
repeated violation identities even when their severities differ.

Optional `positiveControls` pin source facts that must stay in a fixture but
must not appear in engine violation output. Each control has `rule` and `from`.
Only `package-main-placement` controls use a source `packageName` and omit
`to`; other controls, including `no-orphans`, represent an import fact in `to`
and omit `packageName`. The unreachable fixture does not use source-only
positive controls. Controls are sorted and checked as strictly as violations.
They keep allowed imports, exceptions, and allowed package roots from
disappearing while a violation-only comparison still passes. A control and
violation may not claim the same rule/source/target identity.

The harness also derives all disconnected files and all `package main` files
outside `cmd/` and `tools/` from the parsed fixture. It rejects any such
source-only violation that is not listed in the golden, so adding another bad
file cannot silently weaken these cases.

The `baseline-expiry` module also pins baseline matching and expiry with two
additional artifacts. `baseline.json` is an input containing exact raw import
keys. `baseline.golden.json` projects the expected three states: `violations`
for unmatched live violations, `known` for matched live violations, and
`stale` for baseline entries whose violation disappeared. Stale entries add
the exact diagnostic telling the user which entry to remove. These artifacts
remain separate from `violations.golden.json`, whose import target is the
resolver-normalized path used by the engine corpus.

The corpus deliberately does not include rule configuration files. Issue #3
owns that format. Engine tests can construct rules through the eventual Go API,
run each module as its scan root, project results onto this stable structure,
and compare them with the golden list.

## Cases

| Directory | Case pinned by the module |
|---|---|
| `baseline-expiry` | A raw-path baseline match preserves the live violation's configured severity while a removed import becomes a stale error with an exact deletion diagnostic. |
| `layer-direction` | Core may import core and a pinned migration file may import infra, but another core-to-infra edge is rejected. |
| `number-of-dependents` | Files in the leaf package are reported below two direct dependent packages, while importer file splitting and a same-directory external test import do not inflate the hub count. |
| `stdlib-denylist-exception` | Exact stdlib bans honor a package/import exception without exempting sibling imports. |
| `third-party-in-core` | Core rejects a third-party module dependency. |
| `stdlib-only-tree` | A tools tree may use stdlib but rejects a local dependency. |
| `forbidden-import-target` | Product code rejects imports of a designated entrypoint tree. |
| `orphan-file` | A disconnected file is reported while connected files are not. |
| `package-main-placement` | `package main` is rejected outside approved command and tool roots. |
| `reachable-test-helper` | Of two files in one package, only the file whose initiating import transitively reaches test helpers is rejected. |
| `required-dependency` | Each matching handler file must import the logging package; an importless file violates while a compliant sibling is retained as a positive control. |
| `unclassified-dependency` | An allowed-rule set fails closed on an unclassified local dependency. |
| `unreachable-dead-code` | Every file in a package outside the entrypoint package closure is reported while live packages remain clean. |

The first eight semantic cases are owned by issue #4; `baseline-expiry` is the
ninth case and is owned by issue #6; `required-dependency` is the tenth and is
owned by issue #24; `number-of-dependents` is the eleventh and is owned by
issue #28; the reachable and unreachable cases are the twelfth and thirteenth
and are owned by issue #27. They are inspired by
fanout's architecture checks but are not a one-to-one copy of its test
functions; filesystem tree-shape checks remain outside the import graph's
scope.
