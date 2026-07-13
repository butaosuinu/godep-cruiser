# fanout architecture-test snapshot

This reduced module snapshots the observable rules in fanout's
`internal/arch/arch_test.go` at commit
`b20d79497896adc99b09387b0c9f262ab5730375` (blob
`fe25b893e97fdf967001047b96d852b4f817c738`). It is synthetic: the files keep
the layer names, representative legal edges, and deliberately invalid
sentinels needed for parity testing without copying fanout implementation code.

The five files under `configs/` translate the import-graph checks. The baseline
suppresses the live `internal/infra/team/path_test.go` exception and contains a
second, removed edge so stale-entry expiry remains observable. `oracle.golden.json`
records the hand-written oracle projection in the upstream test order.

`cmd/unclassified.go` deliberately lives directly in `cmd/` and imports the
same package path. fanout classifies only `cmd/<subdirectory>` packages, so the
source and target both collapse to the single unclassified subject `cmd`.

`tools/unclassified.go` deliberately lives directly in `tools/`, has no
imports, and is not imported. The parity harness adds a source-only probe rule
through the public configuration API so this isolated package is still
enumerated and reported as the unclassified subject `tools`.

`internal/retired/README.md` intentionally leaves a non-Go-only directory below
`internal/`. fanout's `TestInternalTreeShape` sees that directory, while
godep-cruiser intentionally scans Go source only; the oracle records it as an
expected gap rather than an unexpected parity failure.
