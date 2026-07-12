# godep-cruiser 設計ドキュメント(2026-07)

Go ソース向けの依存ルール検証ツール godep-cruiser を開発する。
[dependency-cruiser](https://github.com/sverweij/dependency-cruiser) の概念
(forbidden / allowed / required、from/to の regex マッチ、dependencyTypes、
baseline)を Go に移植し、本家にもない「未使用 baseline エントリの自動失効」を
加える。この文書はプロジェクト立ち上げ時の検討結果を固めた設計の正典で、
v0.1 の実装は issue ツリーで管理する。

## 背景

fanout の決定記録(`docs/arch-test-tools.ja.md`、2026-07)は、既存の Go
アーキテクチャテストツール(go-arch-lint / arch-go / depguard)を調査し、
fail-closed 分類・ファイル単位の例外・stdlib の完全一致制限・未使用例外の
自動失効を同時に満たすものがないと結論した。dependency-cruiser はこの要求
空間のほぼ全てをルールモデルで表現できるが、対応言語は JS/TS/CoffeeScript
のみで Go を解析できない。多言語ツールの dep-tree も Go を解析対象にして
おらず、ルールは glob の allow/deny にとどまる。「dependency-cruiser 相当の
Go ツール」というニッチは空いている。

なお本プロジェクトは汎用の Go 依存ルール検証ツールとして設計する。fanout での
採用は非ゴール(fanout の 8 テストは参照要求・検証コーパスとして使う。後述)。

## 本家との関係

- クリーンルーム再実装。JS コードの翻訳はせず、公開ドキュメントと観察可能な
  挙動から再設計する
- ライセンスは MIT(本家と同じ)
- README で dependency-cruiser を明示的にクレジットし、概念の借用を隠さない
- ツール名に "dependency-cruiser" / "depcruise" を含めない(本家 CLI 名
  `depcruise` との混同回避と商標的礼儀)

## 要求仕様(汎用)

1. Go module のソースツリーを走査し、ファイル単位の import 依存グラフを作る
2. 依存に対する forbidden / allowed ルールを regex(`path` / `pathNot`、配列可)
   で書ける。allowed は fail-closed(どの allowed にもマッチしない依存は違反)
3. 依存種別(stdlib / module 内 / third-party / 解決不能)でマッチできる。
   stdlib は `^os$` のような完全一致 regex で個別パッケージを制限できる
4. grandfathered 違反を baseline に記録して新規違反だけを検出できる。
   **baseline エントリに対応する違反が消えたら、それ自体をエラーにする**(自動失効)
5. 違反メッセージは修正方法まで示す(「どのルールに、どの edge が、なぜ」)
6. exit code = error 違反数。CI にそのまま置ける

### 参照ケース: fanout の 8 テスト

fanout の `internal/arch/arch_test.go` は本ツールが表現すべき典型要求の実例
(層方向 + ファイル単位例外、stdlib denylist + パッケージ例外、stdlib-only
ツリー、被 import 禁止、fail-closed 分類、自動失効)。設計検証では 8 テストを
本ツールの設定に翻訳し、検出結果を突き合わせる。ただし fanout での置換・採用は
このプロジェクトの目標に含めない。実ディレクトリ形状検査
(`TestInternalTreeShape` 相当)は import グラフの守備範囲外として non-goal。

## 概念マッピング表

本家 rules-reference の全属性を「移植 / Go 再解釈 / 捨てる(JS 固有)/
non-goal」に分類する。

### ルールレベル

| 属性 | 分類 | 備考 |
|---|---|---|
| name / comment | 移植 | forbidden / allowed の各ルールに必須の name と任意の comment を置く |
| severity (error, warn, info, ignore) | 移植 | forbidden はルール単位、allowed の未一致違反はトップレベルの allowedSeverity を使う |
| scope (module / folder) | 一部移植 | v0 は module 固定を暗黙適用し、scope フィールドと folder scope は将来追加する |

### from 側

| 属性 | 分類 | 備考 |
|---|---|---|
| path / pathNot | 移植 | regex 配列。ファイル単位粒度 |
| orphan | 移植 | 依存グラフの孤立ファイル検出 |
| numberOfDependentsLessThan / MoreThan | 後回し | metrics 系。v0.2 以降 |
| (新規) packageName | Go 固有追加 | package 節の regex マッチ。package main の配置制限などに使う |

### to 側

| 属性 | 分類 | 備考 |
|---|---|---|
| path / pathNot | 移植 | |
| dependencyTypes / dependencyTypesNot | Go 再解釈 | `core`→`stdlib`、`local`→`local`(module 内)、`npm` 系→`module`(third-party)、`couldNotResolve`→`unresolved` の 4 分類 |
| couldNotResolve | Go 再解釈 | dependencyTypes の `unresolved` に統合 |
| reachable | 後回し | required ルールと併せて v0.2 以降 |
| ancestor | 後回し | 需要が見えてから |
| circular / via / viaOnly | **non-goal(恒久)** | Go コンパイラが import cycle を禁止するため原理的に不要。本家の看板機能を丸ごと削れるのが Go 移植の大きなスコープ削減 |
| moreThanOneDependencyType | 捨てる | Go では依存分類が排他的で成立しない |
| license / licenseNot | 捨てる | npm メタデータ前提。Go の依存ライセンス検査は別ツールの領分 |
| dynamic / exoticallyRequired / exoticRequire / exoticRequireNot / preCompilationOnly | 捨てる | JS/TS 固有(動的 import・require ラッパー・TS プリコンパイル) |
| moreUnstable / numberOfDependents* | 後回し | metrics 系。v0.2 以降 |

cruise オプション(doNotFollow / includeOnly / exclude / focus 等)は v0 では
scan root 指定と skip 規則(`testdata/`、`_`・`.` 接頭ディレクトリ、`vendor/`)
に絞る。

### 設定形式と loader

v0.1 の設定形式は JSON のみとし、YAML は受理しない。
標準ライブラリの `encoding/json` だけで loader を実装でき、ランタイム依存を追加せずに未知フィールドと入力位置を検証できるためである。
公開する JSON Schema は [`schema/godep-cruiser.schema.json`](schema/godep-cruiser.schema.json) とし、Go の `regexp` 構文の検証と capture の相互参照は loader が担う。

トップレベルには `forbidden`、`allowed`、`allowedSeverity` だけを置ける。
`allowed` を省略すると allowed 検査を無効にし、空配列を明示するとすべての依存を拒否する fail-closed 検査になる。
各ルールは空でない `name` と `from` と `to` を必須とし、`{}` のような空ルールや metadata だけのルールを拒否する。
同じ `forbidden` または `allowed` 配列内で rule name が重複する設定も拒否する。
一方、`from: {}` と `to: {}` を明示したルールは全対象に一致する catch-all として受理する。
forbidden ルールで `from.orphan` または `from.packageName` を指定し、かつ `to: {}` の場合だけ source-only として edge を評価せず、一致したファイルごとに 1 件の違反を生成する。
to 条件を持つ forbidden ルール、allowed ルール、`from: {}` と `to: {}` の catch-all は edge rule として評価する。
forbidden ルールの `severity` とトップレベルの `allowedSeverity` は、省略時に `warn` とする。
allowed ルールに個別の `severity` は置けないため、どの allowed ルールにも一致しなかった依存には `allowedSeverity` を使う。
allowed 未一致違反の rule 名は予約名 `not-in-allowed` に固定し、forbidden または allowed の rule name に同名を指定した設定は loader が拒否する。
`severity: "ignore"` の forbidden ルールと `allowedSeverity: "ignore"` の allowed 検査はエンジンが評価を省略し、違反リストに含めない。
これは baseline 照合後の報告上の ignore とは別の機構である。

`path`、`pathNot`、`packageName` は Go の正規表現を文字列配列で指定する。
同じフィールド内の配列要素は OR、異なるフィールド間は AND として評価する。
正規表現フィールドに単一文字列、`null`、空配列を指定した設定は受理しない。
`dependencyTypes` と `dependencyTypesNot` も配列とし、値を `stdlib`、`local`、`module`、`unresolved` に限定する。

`to.path` と `to.pathNot` では、`from.path` の capture group を `$1`、`$2` のように参照できる。
複数の `from.path` が一致した場合は、宣言順で最初に一致した正規表現の capture を使う。
参照する group はすべての `from.path` 候補に存在しなければならず、loader は存在しない参照を設定エラーにする。
展開する capture 値は正規表現リテラルとして escape し、path に含まれる記号が `to` 側の正規表現構文へ変わることを防ぐ。
リテラルの `$1` は `\$1` と書き、文字クラス内の capture 参照、`$0`、`${1}`、名前付き参照は v0.1 では受理しない。

loader は JSON の構文エラーをファイル名、行、列つきで返す。
型違い、重複フィールド、未知フィールド、不正な正規表現、不正な enum、不正な capture 参照には JSON path も付ける。
`required`、`scope`、`reachable`、`couldNotResolve` などの v0.1 対象外フィールドも、互換性のために読み捨てず未知フィールドとして拒否する。

## コア設計判断

### スキャナ: go/parser の全ファイル走査

`go/parser` + `parser.ImportsOnly` で scan root 以下の全 `.go` を parse する。
build constraint を評価しないので、build tag や OS suffix で現在の
GOOS/GOARCH から外れるファイルも `_test.go` も検査対象になる。
skip 規則は scan root 配下のディレクトリに適用し、明示された root 自体は
名前にかかわらず走査する。
`packages.Load` 系ツールの検査漏れ(arch-go / depguard の不採用理由の一つ)を
最初から避ける。型検査はしない。実装は stdlib のみ。

resolver は go.mod の module path を読み、stdlib 判定(先頭セグメントに
ドットなし)、module 内 import の相対化、それ以外を third-party とする。
`import "C"`(cgo)は Go package を指さない擬似 import のため `unresolved`
として保持する。resolver は呼び出し側が明示した単一の go.mod だけを読み、
go.work と入れ子 module の自動探索・切り替えは行わない。multi-module /
go.work は v0 対象外。

### 自動失効: baseline の 3 状態セマンティクス

差別化の本体として、本家 baseline にはない stale 検出を加える。
baseline は既知違反の抑止と失効検出だけを担い、ルールに設定済みの severity を変更しない。

- baseline にない現在の違反 → 設定済み severity のまま報告する
- baseline にある現在の違反 → known として通常の違反報告から抑止する
- **現在の違反に対応しない baseline エントリ → severity にかかわらず stale error にする**(本家にない新規部分)。
  診断では、そのエントリを baseline から削除するよう示す

baseline はトップレベルを `{ "entries": [...] }` とする JSON であり、各エントリに `rule`、`from`、任意の `to` を置く。
edge 違反の完全一致キーは、ルール名、from ファイルパス、ソースに記述されたままの raw import path で構成する。
raw import path は `to` に記録する。
resolver が返す path は module path の変更で変わり得るため、baseline のキーには使わない。
orphan や packageName の source-only 違反は `to` を省略し、ルール名と from ファイルパスで照合する。
生成時はキー順にソートして重複を除き、読み込み時は未知フィールド、空のキー、重複キー、末尾の余分な JSON を拒否する。
stale エントリは削除済みのファイルや import を指し得るため、読み込み時に参照先の存在を要求しない。

失効判定を決定可能にするため、baseline エントリは regex ではなく完全一致で記録する。
regex を許すと複数のエントリが同じ違反に重なり、どの例外がその違反を保持しているかが一意に決まらない。
完全一致キーなら、現在の違反キーと baseline エントリの差集合として stale を判定できる。
`//nolint` 型のコメントディレクティブには対応せず、例外を設定と baseline に集約して失効検出を一元化する。
expiry date(期限日)方式も採用しない。
対応する違反が消えた時点で stale error になるため、日付による期限は不要である。

### 空回りの fail-closed

scan root ごとに「1 ファイル以上 parse した」ことをツールの既定動作として
保証する(0 件なら設定エラーで失敗)。壊れた glob や移動済みディレクトリを
指したままの設定が silent pass にならない。

### 配布: library-first + 薄い CLI

エンジンを importable package として設計し、CLI は薄い wrapper にする。
`go test` から `archtest.Check(t, cfg)` の形で呼べる helper への道を残す
(helper 自体は v0.1 必須ではない)。golangci-lint plugin は対象外
(module plugin の制約が重く、需要が見えてから)。

## MVP スコープ(v0.1)

| コンポーネント | v0.1 | 備考 |
|---|---|---|
| scanner(全ファイル parse)+ resolver | IN | stdlib のみで実装 |
| forbidden / allowed(fail-closed)/ orphan | IN | path / pathNot 配列、from グループキャプチャ `$1`、dependencyTypes、severity |
| packageName マッチ + scan root 空回り検出 | IN | Go 固有追加 |
| baseline + 自動失効 | IN | 差別化の本体 |
| reporter: err / json / mermaid | IN | err は修正方法つきメッセージを設計目標にする |
| required ルール / reachable | OUT → v0.2 | |
| cache | OUT | go/parser は全 parse でも十分速い(fanout は毎テストバイナリで全走査して問題ない実績) |
| metrics(instability)/ ancestor / folder scope | OUT | |
| dot / HTML / CSV / teamcity reporter | OUT | |
| circular | OUT(恒久) | |

規模見立て: 2〜4k 行 + テスト。

## 検証戦略

1. ルール種別ごとに既知違反を仕込んだ testdata module 群を作り、table-driven
   テストで検出の網羅を固定する(新リポジトリ内で完結)
2. fanout の 8 テストを本ツールの設定に翻訳し、`internal/arch` の検出結果と
   突き合わせる parity 検証を検証ケースの一つにする(fixture は fanout の
   層構造を写した snapshot。fanout 側の CI には触れない)
3. 本家ドキュメントのルール実例を数件 Go に翻訳し、概念マッピングが実際に
   成立することを確認する

## リスク

- dep-tree が tree-sitter で Go 対応を足す可能性はあるが、glob allow/deny
  止まりで、stdlib 分類・go.mod 解決・build tag 非依存の全ファイル parse・
  自動失効という Go 意味論の堀は残る
- 本家は 388 リリース分の機能表面を持つ。上の MVP 切断線とマッピング表の
  「捨てる」分類で防御する
- 単独メンテ。まず自分のリポジトリ群で使える最小機能に絞る

## ツール名(決定: godep-cruiser)

2026-07-10 の空き確認(GitHub リポジトリ名検索)で候補を比較し、
godep-cruiser に決定した(同名ヒット 0 件・完全空き)。検討した候補:
gocruise(4 件、最大 0★)/ depwarden(3 件、最大 2★ Python)/
layerlint(機能より狭い名前)。importlint(zchee/go-importlint と同ニッチ
衝突)・depsentry(23★ Rust)・depcruise 系(本家 CLI 名)は回避した。
pkg.go.dev はモジュールパス(github.com/butaosuinu/godep-cruiser)単位なので
衝突しない。

## ロードマップ案

- v0.1: 上記 MVP(issue ツリーで管理)
- v0.2: required + reachable、cache、numberOfDependents 系
- v0.3: `go test` helper、reporter 拡充(dot / HTML)、folder scope

リポジトリは github.com/butaosuinu/godep-cruiser(public)。

## 残る open questions

- 実装着手時期(fanout ロードマップとの優先順位)

## 参考

- https://github.com/sverweij/dependency-cruiser — v18.0.0(2026-06)、rules-reference / cli ドキュメント
- fanout `docs/arch-test-tools.ja.md` — 既存 Go ツールの不採用決定記録(要求の源泉)
- fanout `internal/arch/arch_test.go` — scanner・stdlib 判定・自動失効セマンティクスの参照原型
- https://github.com/gabotechs/dep-tree — 多言語依存ツール(Go 解析非対応の確認元)
