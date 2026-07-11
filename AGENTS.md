# Repository Guidelines

## Project Structure and Sources of Truth

`cmd/godep-cruiser/` contains the deliberately thin CLI. Put reusable validation logic in importable packages rather than growing `package main`; the exact package layout is owned by the implementation issues. `DESIGN.ja.md` is the canonical v0.1 design, `README.md` is the public overview, and GitHub issues #1-#9 define scope and dependency order. If an implementation changes a design decision, update `DESIGN.ja.md` in the same change.

This project is a clean-room Go reimplementation of dependency-cruiser concepts. Use only public documentation and observable behavior; do not translate upstream JavaScript. Modifying or adopting the tool in fanout is outside this repository's scope.

## Build, Test, and Development Commands

- `make build` compiles the CLI to the gitignored root binary.
- `make test` runs all Go unit tests.
- `make lint` runs the pinned golangci-lint configuration.
- `make check` is the canonical pre-push quality gate and runs `make test` followed by `make lint`.
- `make fmt` applies gofumpt and goimports through that pinned tool.
- `make vuln` runs govulncheck; it requires network access and is intentionally separate from required lint checks.
- `make clean` removes the built binary.

Build and lint caches live under `.cache/`. Do not replace the pinned tool versions with globally installed variants when verifying changes.

## Agent Hooks

On macOS and Linux, repository hooks for Claude Code and Codex run `make check` before a supported agent shell call that invokes `git push` and block the push when tests or lint fail. After agent `Write`/`Edit` operations (including Codex `apply_patch`), they run the repository-wide `make fmt`. `make vuln` is deliberately excluded from the pre-push gate.

Codex project hooks must be trusted through `/hooks` before they run. Codex currently intercepts only supported, simple shell paths, so alternate tool paths can bypass `PreToolUse`. These POSIX hooks do not cover Windows or regular-terminal pushes and are not native Git hooks; CI remains the final backstop.

## Coding and Architecture Conventions

Target Go 1.25. Production implementation must remain standard-library-only; dependencies recorded for `go tool` commands are development tooling, not runtime libraries. Preserve the library-first, thin-CLI design and document exported APIs.

The scanner must inspect every `.go` file with `go/parser` and `parser.ImportsOnly`, including `_test.go`, build-tagged, and platform-suffixed files. Keep unresolved dependencies explicit, make allowed rules fail closed, fail when a scan root parses zero Go files, and treat stale exact-match baseline entries as errors. Do not pre-empt issue-owned decisions such as configuration format, `import "C"` classification, or the final library package layout.

## Testing Guidelines

Use the standard `testing` package and table-driven tests with a descriptive `name` field and `t.Run`; use `t.Parallel` where isolation permits. Place intentionally invalid dependency graphs under `testdata/` and use golden fixtures for stable diagnostics and reporter output. Before handoff, run `make fmt` and `make check`.

## Commit and Pull Request Guidelines

Keep each change focused on its issue and use a concise commit subject. Pull requests should link the issue, summarize behavior and design-document changes, and list the verification commands run. Update `README.md` when user-facing CLI behavior changes.
