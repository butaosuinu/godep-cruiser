# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト概要

godep-cruiser は Go ソースツリー向けの依存ルール検証ツール。
[dependency-cruiser](https://github.com/sverweij/dependency-cruiser) の概念
(forbidden / allowed / required ルール、regex `path` / `pathNot` マッチ、推移到達、
被依存数、依存種別分類、baseline)の
クリーンルーム Go 再実装に、本家にない「baseline エントリの自動失効」を加える。

**現状: v0.2 の required、reachable、numberOfDependents 系まで実装完了し、`v0.2.0` としてリリース済み。**
scanner、設定 loader、file と package の依存グラフ、rule engine、baseline、reporter、
公開 API / CLI、parity 検証まで main に入っている。
[DESIGN.ja.md](DESIGN.ja.md) が設計の正典。
設計レベルの判断をする前に必ず DESIGN.ja.md を読むこと。

## ハード制約

- **クリーンルーム**: dependency-cruiser のソースコードを読まない・取得しない・翻訳しない。
  設計の根拠にしてよいのは公開ドキュメントと観察可能な挙動のみ
- **エンジンは stdlib のみで実装**(third-party 依存禁止)。現在 go.mod にある依存は
  govulncheck の tool directive 由来のみで、エンジンの依存ではない
- ツール名・バイナリ名・パッケージ名に "dependency-cruiser" / "depcruise" を含めない

## コマンド

| コマンド | 内容 |
|---|---|
| `make build` | CLI バイナリ `godep-cruiser` をビルド(gitignored) |
| `make test` | `go test ./...` |
| `make lint` | pinned golangci-lint v2 で `run`(初回は `.cache/tools/` に自動インストール) |
| `make check` | エージェント用の正典となる品質ゲート。`make test` → `make lint` の順で実行 |
| `make fmt` | gofumpt + goimports(`golangci-lint fmt`) |
| `make vuln` | govulncheck。ネットワークで脆弱性 DB を取得するため意図的に lint gate 外 |

- 単一テスト: `go test -run 'TestName' ./cmd/godep-cruiser`(パッケージパスは対象に合わせる)
- golangci-lint のバージョンは `.golangci-lint-version` が単一ソース(CI も同じファイルを参照)
- ビルド・lint キャッシュはリポジトリローカル `.cache/`(gitignored)に置かれ、グローバルキャッシュを汚さない
- CI: `.github/workflows/test.yml` が `make test` + golangci-lint を実行。`vuln.yml` は
  コード変更なしでも新 CVE で落ちうるため required check にしない方針(workflow 内コメント参照)

## エージェント hooks

- macOS / Linux で Claude Code / Codex の `PreToolUse` hook が `Bash` 入力を固定文字列
  `git push` で `grep` し、一致した場合だけ `make check` を直接実行する。test または lint が
  失敗した場合は tool call を拒否してコマンド出力をエージェントへ返す。ネットワーク依存の
  `make vuln` は対象外
- `Write` / `Edit`（Codex の `apply_patch` を含む）の後は `PostToolUse` hook がリポジトリ全体へ
  `make fmt` を直接実行する
- pre-push hook は shell 構文を解析せず、literal な `git push` だけを最小限のfilterで判定する
- Codex の project hook は `/hooks` からリポジトリを trust して有効化する
- この POSIX hooks は Windows、未対応の tool 経路、literal でないpush表現、通常のターミナル、
  native Git hook には適用されないため、CI を最終的な品質ゲートとして維持する

## アーキテクチャ(DESIGN.ja.md の要点)

- **library-first + 薄い CLI**: `cruiser` が公開 facade。module root と
  `cmd/godep-cruiser` は同じ shared runner を呼ぶ薄い wrapper で、root は
  `go install github.com/butaosuinu/godep-cruiser@latest` を成立させる
- **CLI パターン**: `main()` は一行、実体は testable な `run(args, stdout, stderr) int` に置く
  (プロセスを spawn せずテストできる)
- **scanner**: `go/parser` + `parser.ImportsOnly` で scan root 以下の全 `.go` を parse。
  build constraint を評価しないので、build tag / OS suffix で外れるファイルも `_test.go` も
  検査対象。型検査なし、`packages.Load` 不使用。skip 規則: `testdata/`、`_`・`.` 接頭
  ディレクトリ、`vendor/`
- **依存 4 分類(排他的)**: `stdlib`(先頭セグメントにドットなし。`^os$` のような完全一致
  regex で個別パッケージを制限可)/ `local`(module 内)/ `module`(third-party)/ `unresolved`
- **ルール**: forbidden / allowed(fail-closed: どの allowed にもマッチしない依存は違反)/
  required(一致する各 file に必須 import を要求)。orphan、Go 固有の `packageName`、package の
  direct dependent 数を照合する `numberOfDependentsLessThan` / `MoreThan` を from 条件に使える。
  forbidden の `to.reachable` は local package graph の推移到達と非到達を検査する。
  regex `path` / `pathNot` は配列可、ファイル単位粒度、from グループキャプチャ `$1` 対応。
  severity は error / warn / info / ignore
- **baseline 3 状態セマンティクス(差別化の本体)**:
  baseline にない違反 → 設定済み severity のまま報告、baseline にある違反 → known として抑止、
  **違反が消えた baseline エントリ → stale error**(自動失効)。
  エントリは regex でなく完全一致で記録する。
  通常の edge 違反は from ファイルパス + raw import パス + ルール名、reachable 違反は raw import の
  代わりに target package path を使い、source-only 違反は to を省略する。
  `//nolint` 型のコメントディレクティブと expiry date は不採用 — 例外は設定と baseline に集約する
- **空回りの fail-closed**: scan root で 1 ファイルも parse できなければ設定エラー
  (壊れた glob の silent pass を防ぐ)
- **reporter**: err(修正方法まで示すメッセージが設計目標)/ json / mermaid。
  exit code = 未抑止の error 違反数 + stale baseline entry 数(255 を上限とする)

### non-goal

- **circular 検出は恒久 non-goal**(Go コンパイラが import cycle を禁止するため原理的に不要)
- cache は 10k ファイルの実測が再検討閾値未満で、失効条件が不完全な cache は fail-closed 方針と
  矛盾するため見送る
- v0.3 候補: `go test` helper、dot / HTML reporter、folder scope、moreUnstable、
  `internal/engine` と `cruiser` の `ViolationKind` 二重定義の整理
- ancestor、CSV / teamcity reporter、multi-module / go.work は需要が見えてから検討する
- fanout での採用は非ゴール。fanout の `internal/arch` 8 テストは parity 検証コーパスとしてのみ使う

## lint 基盤が強制する規約

- 公開 API に doc comment 必須(revive `exported` / `package-comments`)
- `//nolint` は理由と linter 指定が必須(nolintlint)。`init()` 禁止(gochecknoinits)
- `testdata/` は golangci-lint がデフォルト除外 → 既知違反を仕込む testdata module 群の
  置き場として意図されている
- 不採用 linter とその根拠は `.golangci.yml` 冒頭コメントに記録されている。linter を
  足す・外すときはそこを読んでから

## 検証戦略

1. ルール種別ごとに既知違反を仕込んだ testdata module 群 + table-driven テストで検出の網羅を固定
   (`testdata/corpus/` + `internal/testcorpus`)
2. fanout の 8 テストを本ツールの設定に翻訳し、検出結果を突き合わせる parity 検証
   (実装済み: `cruiser/parity_test.go` + `testdata/parity/`。既知ギャップは DESIGN.ja.md に記録)
3. 本家ドキュメントのルール実例を数件 Go に翻訳し、概念マッピングの成立を確認
