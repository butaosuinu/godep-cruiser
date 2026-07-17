# godep-cruiser 設計ドキュメント(2026-07)

Go ソース向けの依存ルール検証ツール godep-cruiser を開発する。
[dependency-cruiser](https://github.com/sverweij/dependency-cruiser) の概念
(forbidden / allowed / required、from/to の regex マッチ、dependencyTypes、
baseline)を Go に移植し、本家にもない「未使用 baseline エントリの自動失効」を
加える。この文書はプロジェクト立ち上げ時の検討結果を固めた設計の正典で、
v0.1 以降の実装は issue ツリーで管理する。

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
6. exit code = 未抑止の error 違反数 + stale baseline entry 数(255 を上限とする)。CI にそのまま置ける

### 参照ケース: fanout の 8 テスト

fanout の `internal/arch/arch_test.go` は本ツールが表現すべき典型要求の実例
(層方向 + ファイル単位例外、stdlib denylist + パッケージ例外、stdlib-only
ツリー、被 import 禁止、fail-closed 分類、自動失効)。設計検証では 8 テストを
本ツールの設定に翻訳し、検出結果を突き合わせる。ただし fanout での置換・採用は
このプロジェクトの目標に含めない。実ディレクトリ形状検査
(`TestInternalTreeShape` 相当)は import グラフの守備範囲外として non-goal。

parity fixture の oracle は fanout の commit
`b20d79497896adc99b09387b0c9f262ab5730375` にある
`internal/arch/arch_test.go` (blob
`fe25b893e97fdf967001047b96d852b4f817c738`)に固定する。

| fanout のチェック | godep-cruiser での翻訳 | parity 結果 |
|---|---|---|
| `TestAllPackagesClassified` | source-only probe による全 package 列挙 + allowed 設定の fail-closed 違反 `not-in-allowed` | 一致 |
| `TestInternalTreeShape` | 非 Go ファイルを含む実ディレクトリ形状は走査対象外 | 期待済みギャップ |
| `TestExplicitLayerMapIsCurrent` | `internal/arch` を明示した scan root として検証し、Go ファイル 0 件を失敗させる | 一致 |
| `TestLayerImportDirection` | `layer-import-direction` と完全一致 baseline によるファイル単位例外および自動失効 | 一致 |
| `TestCorePurity` | `core-stdlib-denylist` と `core-third-party` | 一致 |
| `TestToolsStdlibOnly` | `tools-stdlib-only` | 一致 |
| `TestPackageMainOnlyInCmd` | `package-main-placement` と `cmd-not-importable` | 一致 |
| `TestScanSanity` | `internal`、`cmd`、`tools` を独立した scan root として検証 | 一致 |

7 チェックは設定または明示した scan root で oracle と一致した。
期待済みギャップは `TestInternalTreeShape` の 1 件だけで、想定外ギャップは 0 件だった。
この検証結果は fanout での採用判断を含まない。

## 概念マッピング表

本家 rules-reference のルール種別と属性を「移植 / Go 再解釈 / 捨てる(JS 固有)/
non-goal」に分類する。

### ルールレベル

| 概念 | 分類 | 備考 |
|---|---|---|
| forbidden / allowed / required | 移植 | required は v0.2 で移植済み |
| name / comment | 移植 | forbidden / allowed / required の各ルールに必須の name と任意の comment を置く |
| severity (error, warn, info, ignore) | 移植 | forbidden / required はルール単位、allowed の未一致違反はトップレベルの allowedSeverity を使う |
| scope (module / folder) | 移植 | v0.3 で forbidden に移植済み。省略時は module とする |

### from 側

| 属性 | 分類 | 備考 |
|---|---|---|
| path / pathNot | 移植 | regex 配列。module scope ではファイル座標、folder scope では package 座標に一致させる |
| orphan | 移植 | 依存グラフの孤立ファイル検出 |
| numberOfDependentsLessThan / MoreThan | 移植 | v0.2 で移植済み。package の direct dependent 数を照合する |
| (新規) packageName | Go 固有追加 | package 節の regex マッチ。package main の配置制限などに使う |

### to 側

| 属性 | 分類 | 備考 |
|---|---|---|
| path / pathNot | 移植 | |
| dependencyTypes / dependencyTypesNot | Go 再解釈 | `core`→`stdlib`、`local`→`local`(module 内)、`npm` 系→`module`(third-party)、`couldNotResolve`→`unresolved` の 4 分類 |
| couldNotResolve | Go 再解釈 | dependencyTypes の `unresolved` に統合 |
| reachable | Go 再解釈 | v0.2 で移植済み。forbidden の from file から target package への推移到達と、entrypoint package 群から到達できない package の検出に使う |
| ancestor | 後回し | 需要が見えてから |
| circular / via / viaOnly | **non-goal(恒久)** | Go コンパイラが import cycle を禁止するため原理的に不要。本家の看板機能を丸ごと削れるのが Go 移植の大きなスコープ削減 |
| moreThanOneDependencyType | 捨てる | Go では依存分類が排他的で成立しない |
| license / licenseNot | 捨てる | npm メタデータ前提。Go の依存ライセンス検査は別ツールの領分 |
| dynamic / exoticallyRequired / exoticRequire / exoticRequireNot / preCompilationOnly | 捨てる | JS/TS 固有(動的 import・require ラッパー・TS プリコンパイル) |
| moreUnstable | 後回し | metrics 系。v0.3 候補 |

cruise オプション(doNotFollow / includeOnly / exclude / focus 等)は v0 では
scan root 指定と skip 規則(`testdata/`、`_`・`.` 接頭ディレクトリ、`vendor/`)
に絞る。

### 設定形式と loader

設定形式は v0.1 から JSON のみとし、YAML は受理しない。
標準ライブラリの `encoding/json` だけで loader を実装でき、ランタイム依存を追加せずに未知フィールドと入力位置を検証できるためである。
公開する JSON Schema は [`schema/godep-cruiser.schema.json`](schema/godep-cruiser.schema.json) とし、Go の `regexp` 構文の検証と capture の相互参照は loader が担う。

トップレベルには `forbidden`、`required`、`allowed`、`allowedSeverity` だけを置ける。
`allowed` を省略すると allowed 検査を無効にし、空配列を明示するとすべての依存を拒否する fail-closed 検査になる。
各ルールは空でない `name` と `from` と `to` を必須とし、`{}` のような空ルールや metadata だけのルールを拒否する。
`forbidden` と `required` を合わせた集合、および `allowed` 配列内で rule name が重複する設定も拒否する。
一方、`from: {}` は全 source に一致する catch-all として受理する。
`to: {}` は forbidden / allowed では全依存に一致する catch-all として受理するが、required では要求内容がないため拒否する。
module scope の forbidden ルールで `from.orphan`、`from.packageName`、`from.numberOfDependentsLessThan`、`from.numberOfDependentsMoreThan` のいずれかを指定し、かつ `to: {}` の場合だけ source-only として edge を評価せず、一致したファイルごとに 1 件の違反を生成する。
`to.reachable: false` を指定した forbidden ルールも source-only 違反を生成する。
それ以外の to 条件を持つ forbidden ルール、allowed ルール、`from: {}` と `to: {}` の catch-all は edge rule として評価する。
forbidden ルールの `scope` は `module` または `folder` とし、省略時は `module` とする。
allowed と required に `scope` は指定できない。
folder scope では `dependencyTypes`、`dependencyTypesNot`、`from.orphan`、`from.packageName`、`to.reachable` を拒否する一方、`numberOfDependentsLessThan`、`numberOfDependentsMoreThan` と `from.path` の capture 参照を受理する。
forbidden / required ルールの `severity` とトップレベルの `allowedSeverity` は、省略時に `warn` とする。
allowed ルールに個別の `severity` は置けないため、どの allowed ルールにも一致しなかった依存には `allowedSeverity` を使う。
allowed 未一致違反の rule 名は予約名 `not-in-allowed` に固定し、forbidden / required / allowed の rule name に同名を指定した設定は loader が拒否する。
`severity: "ignore"` の forbidden / required ルールと `allowedSeverity: "ignore"` の allowed 検査はエンジンが評価を省略し、違反リストに含めない。
これは baseline 照合後の報告上の ignore とは別の機構である。

`path`、`pathNot`、`packageName` は Go の正規表現を文字列配列で指定する。
同じフィールド内の配列要素は OR、異なるフィールド間は AND として評価する。
正規表現フィールドに単一文字列、`null`、空配列を指定した設定は受理しない。
`dependencyTypes` と `dependencyTypesNot` も配列とし、値を `stdlib`、`local`、`module`、`unresolved` に限定する。
`reachable` は forbidden の `to` だけで受理し、allowed と required では拒否する。
`reachable` を指定する場合は `to.path` を必須とし、local graph と両立しない `dependencyTypes` と `dependencyTypesNot` の併用を拒否する。
`to.pathNot` は `reachable` と併用できる。

`to.path` と `to.pathNot` では、`from.path` の capture group を `$1`、`$2` のように参照できる。
複数の `from.path` が一致した場合は、宣言順で最初に一致した正規表現の capture を使う。
参照する group はすべての `from.path` 候補に存在しなければならず、loader は存在しない参照を設定エラーにする。
展開する capture 値は正規表現リテラルとして escape し、path に含まれる記号が `to` 側の正規表現構文へ変わることを防ぐ。
リテラルの `$1` は `\$1` と書き、文字クラス内の capture 参照、`$0`、`${1}`、名前付き参照は受理しない。
`reachable: true` は同じ capture 展開を利用する。
`reachable: false` は複数の from file package を seed に集約するため、`to.path` と `to.pathNot` の capture 参照を拒否する。

loader は JSON の構文エラーをファイル名、行、列つきで返す。
型違い、重複フィールド、未知フィールド、不正な正規表現、不正な enum、不正な capture 参照には JSON path も付ける。
`couldNotResolve` などの未対応フィールドも、互換性のために読み捨てず未知フィールドとして拒否する。

## コア設計判断

### required ルール(v0.2)

**required ルール**は、トップレベルの `required` 配列で「`from` に一致する各ファイルが、`to` に一致する import を少なくとも 1 つ持つ」ことを要求する。
各ルールは `name`、任意の `comment`、省略時 `warn` の `severity`、`from`、`to` を持つ。
本家の同種ルールが source 条件に使う `module` は、本実装では `from` とする。
`module` は既に dependency type と go.mod の module という 2 つの意味で使われるため、source selector まで同名にせず Go 向けに再解釈した。

required の `from` には `path`、`pathNot`、`packageName` だけを指定でき、`orphan` は受理しない。
`from: {}` は全ファイルを対象にする catch-all として受理する一方、`to: {}` は要求内容のない退化ルールになるため loader が拒否する。
`to.path` と `to.pathNot` の `$1` 以降の capture 参照は forbidden / allowed と同じ検証・展開機構を使う。
source-only baseline identity `(rule, from)` の衝突を防ぐため、rule name は `forbidden` と `required` の和集合で一意にする。

エンジンは forbidden と allowed に続く第 3 の評価ループで required を処理する。
`from` に一致したファイルごとに import を走査し、`to` に一致するものが 1 件もなければ 1 違反を生成する。
import が 0 件のファイルも要求を満たさないため違反になり、複数の不一致 import があっても違反はファイル単位の 1 件だけになる。
`severity: "ignore"` のルールは評価しない。

required 違反の kind は `required` とし、source-only 形として `to` を持たず、`from.line` には package 節の行を記録する。
baseline JSON の形式は変更せず、rule 名と source ファイルだけの既存 source-only identity で報告、known 抑止、stale error の 3 状態を扱う。

### reachable ルール(v0.2)

**reachable ルール**は forbidden の `to.reachable` で local package graph の推移到達を検査する。
`to.path` と `to.pathNot` は import 文字列や source file path ではなく、module-relative な package path に一致させる。

`reachable: true` は、from に一致した各 file の local import を別々の seed として forward closure を計算する。
closure 内で to に一致した target package ごとに、from file と target package の組を 1 件の edge 違反にする。
違反の `from.line` には到達経路を開始する local import の行を記録するため、開発者は最初に修正すべき import を特定できる。
複数の local import から同じ target package に到達する場合も違反は 1 件に集約し、`from.line` には最小の開始 import 行を使う。
違反の kind は `reachable`、`to.path` は target package path、dependency type は `local` とし、対応する raw import path は空になる。

`reachable: false` は、from に一致した file の package 群を重複のない seed として 1 回の forward closure を計算する。
to に一致する target package が closure に含まれない場合、その package に属する走査済み file を 1 件ずつ違反にする。
違反の kind は `unreachable` とし、`to` を持たない source-only 形で target file の package 節の行を記録する。
package から file へ戻して報告することで、baseline identity を rule 名と file path の組に固定する。

この評価では、matcher の file 粒度と graph の package 粒度を明示的に変換する。
`reachable: true` は from file ごとの import を package seed に変換してから target package を edge 形で返し、`reachable: false` は from file 群を package seed に集約してから unreachable package を file 群へ展開する。

scanner は `_test.go` と build constraint で除外される file も parse するため、それらの local import も package node 間の edge に集約される。
したがって、production file が import する package の test file だけが test helper を import していても、forward closure は test helper への到達を含む。
この混入は `reachable: true` の過剰検出と `reachable: false` の dead code 見逃しを起こし得る。
`from.pathNot` で test file を seed から外しても、推移先 package に集約済みの test edge は除去できない。
既知の test-only target は `to.pathNot` で除外でき、production tree と test tree を別の scan root に分けられる場合は graph 自体を分離できる。
edge を構成した file による推移 edge の filter は、package graph に provenance を保持する必要があるため v0.3 の候補とする。

### 公開型と internal 型の一元化(v0.3)

同じ値を表す named type を境界ごとに再定義せず、依存分類は `config.DependencyType`、違反は `internal/engine` の `ViolationKind`、`Violation`、`Source`、`Dependency`、baseline は `internal/baseline` の `Baseline`、`Entry`、`StaleError` を正本とする。
`internal/scanner.DependencyType` は config 型の alias、`cruiser` の同等な公開型は engine または baseline 型の public alias とし、公開 facade の型名と定数名は維持する。

alias 化は dependency type から先に行う。
engine の `Dependency.Type` が scanner 型を参照したままでも config 型と同一になるため、公開 API で `config.DependencyTypeLocal` などと直接比較できる。
その後に violation と baseline の alias を公開することで、`cruiser` と internal package 間のコピー変換をなくし、validation、baseline、reporter は同じ値を直接受け渡す。

依存方向は scanner から config、cruiser から engine と baseline に限定する。
engine から cruiser を import して公開型を正本にすると循環するため採用しない。
Go の `internal` 制約は import path に対する制約であり、公開 alias を経由した型の利用は妨げない。
公開 `ViolationKind` の定数名と JSON 値 `forbidden`、`not-in-allowed`、`required`、`reachable`、`unreachable` は変更しない。
alias 宣言には `go doc` で単独に読める公開コメントを置く。

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

### package 粒度の依存グラフ

`scanner.File` は、表示と graph identity を分離する**二重座標**を持つ。
`Path` は scan root 相対の user-facing path であり、matcher、baseline、report が従来どおり利用する。
`PackagePath` は module-relative な package directory であり、module root package を `.` とする。
subroot scan でも `PackagePath` と local import の `ResolvedPath` は同じ module-relative 座標を使う。
go.mod から作った resolver はその親ディレクトリを module root とし、module root 外の scan root を拒否する。
module path だけから作った resolver は filesystem root を持たないため、明示された scan root を module root とみなす。

**package 依存グラフ**は、`PackagePath` と観測した local import の `ResolvedPath` を node とし、両者の間に edge を作る。
走査ファイルがない local target も、outgoing edge を持たない leaf node として保持する。
同じ package 間の edge は、import 宣言数やファイル数にかかわらず 1 本に集約する。
stdlib、third-party、unresolved は edge に含めない。

同一ディレクトリの external test package から通常 package への import は self-edge になるため、依存グラフと逆インデックスから除外する。
これにより、direct な fan-in と fan-out は異なる package 間の edge 数になり、ファイル分割の影響を受けない。
ただし orphan 判定は同一ディレクトリの import も被 import として扱うため、全 local import の target を保持する imported view を依存グラフと分離する。

forward と reverse の到達閉包は、既知の seed package を含む multi-source BFS として計算する。
engine はファイルごとの orphan と package 単位の direct dependent 数を `fileFacts` に集約し、from matcher に渡す。
この境界により、reachable と numberOfDependents は scanner の file edge を個別に再集約せず、同じ package 依存グラフを利用できる。

### folder scope(v0.3)

**folder scope**は、forbidden ルールを local package graph の direct edge に適用する評価粒度である。
`from.path` と `from.pathNot` は import 元 package、`to.path` と `to.pathNot` は import 先 package の module-relative path に一致させ、module root は `.` とする。
省略時および `scope: "module"` の従来評価は、scan root 相対の file path と個々の import を照合する。

エンジンは package graph が列挙する各 distinct edge を一度ずつ評価する。
同じ package 間に複数の import 宣言や source file があっても、同じルールから生成する違反は package edge ごとの 1 件になる。
`from.path` の capture は import 元 package path から取得して `to.path` と `to.pathNot` に展開する。
`numberOfDependentsLessThan` と `numberOfDependentsMoreThan` は import 元 package の `FanIn` を使うため、folder scope でも指定できる。

folder scope の違反は kind を `forbidden` のまま維持する。
`From.Path` は import 元 package path、`From.Line` は `0`、`From.PackageName` は空文字列とし、`To.Path` は import 先 package path、`To.Type` は `local`、`To.ImportPath` は空文字列とする。
err reporter は行番号のない package path として表示し、JSON の kind の値を追加しない。

folder scope は local package graph だけを評価するため、`dependencyTypes` と `dependencyTypesNot` を併用できない。
`from.orphan` は file の孤立、`from.packageName` は同一ディレクトリに通常 package と external test package が混在すると一意にならないため拒否する。
`to.reachable` は direct edge ではなく推移到達を評価する別の package 粒度であり、同じルールへの併用を拒否する。
allowed と required には folder 粒度の fail-closed または充足判定を導入せず、`scope` 自体を受理しない。

baseline identity は rule 名、import 元 package path、import 先 package path の完全一致とする。
target に対応する単一の raw import がないため `To.Path` を `to` に記録し、通常の edge と同じ報告、known 抑止、stale error の 3 状態を適用する。

### numberOfDependents(v0.2)

**numberOfDependents** は、source file が属する package の direct dependent 数を `from` 条件として照合する。
`numberOfDependentsLessThan: L` は dependent 数 `n` が `n < L` のときに一致し、`numberOfDependentsMoreThan: M` は `n > M` のときに一致する。
両方を指定した場合は AND 条件となり、`M < n < L` の開区間を使う。

loader は符号、小数点、指数部を含まない非負の JSON 整数だけを受理する。
`numberOfDependentsLessThan` は 1 以上とし、両方の閾値の間に整数が一つも存在しない設定を拒否する。
`LessThan: 0` は常に不一致となり、空の開区間も同じく有効なルールを構成できないためである。

dependent 数は、対象 package を直接 import する distinct な local package directory の数とする。
同じ importer package 内の複数ファイルから import されても 1 と数え、同一 directory の external test package からの import は self-edge として数えない。
各 source file は自身の `PackagePath` に対する `FanIn` を使うため、同じ directory に属するファイルは package 名が通常 package と external test package に分かれていても同じ値を持つ。

module scope の forbidden ルールで dependent 数条件と `to: {}` を組み合わせた場合は source-only 違反とし、既存の `(rule, from)` baseline identity を使う。
folder scope の `to: {}` は全 direct package edge に一致するため、dependent 数条件を満たす import 元 package の outgoing edge ごとに違反を生成する。
`to` に edge 条件がある forbidden ルールと allowed ルールでは、dependent 数条件を importing file 側の絞り込みとして使う。
required ルールの `from` は v0.2 の既存契約どおり `path`、`pathNot`、`packageName` に限定する。

`moreUnstable` は v0.3 へ見送る。
package graph の fan-in と fan-out から instability を計算できるが、ゼロ除算時の値と import 元から import 先への比較方向を先に定義する必要があり、from 単体の dependent 数条件とは設定意味論が異なるためである。

### 自動失効: baseline の 3 状態セマンティクス

差別化の本体として、本家 baseline にはない stale 検出を加える。
baseline は既知違反の抑止と失効検出だけを担い、ルールに設定済みの severity を変更しない。

- baseline にない現在の違反 → 設定済み severity のまま報告する
- baseline にある現在の違反 → known として通常の違反報告から抑止する
- **現在の違反に対応しない baseline エントリ → severity にかかわらず stale error にする**(本家にない新規部分)。
  診断では、そのエントリを baseline から削除するよう示す

baseline はトップレベルを `{ "entries": [...] }` とする JSON であり、各エントリに `rule`、`from`、任意の `to` を置く。
edge 違反の完全一致キーは、ルール名、from 座標、to 座標で構成する。
`To.ImportPath` が空でなければ、ソースに記述されたままの raw import path を `to` に記録する。
resolver が返す path は module path の変更で変わり得るため、この通常 edge の baseline キーには使わない。
`To.ImportPath` が空なら、module-relative な `To.Path` を `to` に記録する。
この一般化により、単一の raw import を持たない reachable 違反と folder scope 違反を同じ規則で扱う。
module scope の orphan、packageName、numberOfDependents と、required、unreachable の source-only 違反は `to` を省略し、ルール名と from ファイルパスで照合する。
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

### cache の再検討条件

`internal/scanner.Scan` と `cruiser.Validate` の性能は、`b.TempDir` に生成した 1,000 ファイルと 10,000 ファイルの合成 module で計測する。
合成 tree は 100 ファイルごとにディレクトリを分け、各 `.go` ファイルに stdlib と module 内の import を 1 件ずつ置く。
tree の生成は計測対象に含めない。

`Scan` の benchmark は resolver を先に構築し、ファイル走査だけを計測する。
`Validate` の benchmark は全合成ファイルに一致する forbidden rule を使い、programmatic 設定の検証、`go.mod` の読み込み、走査、ルール評価を含む通常経路を計測する。
合成 tree に third-party import がないため違反は生成せず、任意機能である baseline は適用しない。

`go test` の package 間の I/O 競合を避けるため `-p=1` で直列化し、`-count=5` で各 benchmark を 5 回計測した各指標から中央値を採った。

```sh
GOTOOLCHAIN=go1.25.8 GOCACHE="$PWD/.cache/go-build" \
  go test -p=1 -run '^$' -bench '^Benchmark(Scan|Validate)$' -benchmem -count=5 \
  ./internal/scanner ./cruiser
```

計測環境は Go 1.25.8、darwin/arm64、Apple M4 で、計測日は 2026-07-15 である。
同じ tree を反復して走査するため、次の値は OS のファイル cache が温まる通常の Go benchmark であり、cold read の値ではない。

| benchmark | Go ファイル数 | time/op 中央値 | B/op 中央値 | allocs/op 中央値 |
|---|---:|---:|---:|---:|
| `BenchmarkScan/files=1000` | 1,000 | 23.56 ms | 2,972,367 | 40,241 |
| `BenchmarkScan/files=10000` | 10,000 | 766.7 ms | 31,079,152 | 401,783 |
| `BenchmarkValidate/files=1000` | 1,000 | 20.33 ms | 3,069,016 | 41,498 |
| `BenchmarkValidate/files=10000` | 10,000 | 772.9 ms | 31,702,240 | 412,080 |

失効条件が不完全な cache は、実際には Go ファイルが 0 件になった scan root に以前の走査結果を返し、空回りを成功扱いし得る。
同じ理由で、削除済みの違反を返し続けると、対応する baseline entry を known のまま残して自動失効を妨げる。
どちらも fail-closed 方針と矛盾するため、現在の実測値だけを理由に cache を導入しない。

**cache 再検討閾値**は、代表的な開発環境で同じ条件の `BenchmarkScan/files=10000` を 5 回計測した time/op の中央値が 3 s/op を超えることとする。
閾値を超え、かつ profile で `Scan` が `Validate` の支配的なボトルネックだと確認できた場合に限り、cache を再検討する。
再検討する場合も、scan root、対象ファイル集合と内容、`go.mod` など、走査結果に影響する全入力の変更で自動失効できることを採用条件とする。

### 配布: library-first + 薄い CLI

エンジンを importable package として設計し、CLI は薄い wrapper にする。
`go test` からは `archtest.Check(t, cfg, opts)` の形で同じ検証経路を呼べる。
golangci-lint plugin は対象外とする(module plugin の制約が重く、需要が見えてから)。

公開 facade は `github.com/butaosuinu/godep-cruiser/cruiser` とする。
`Validate` は programmatic な設定も loader と同じ規則で検証したうえで、明示した
scan root と単一の `go.mod` から scan・rule 評価・任意の baseline 適用を合成し、
未抑止違反・known 違反・stale entry を分けた `Result` を返す。
reporter はこの `Result` を受け、known を出力せず、stale は常に error として err / JSON / Mermaid / GraphViz DOT の各形式と summary に含める。
API は `os.Exit` や暗黙の出力先に依存しない。

Mermaid と DOT は `Result` に含まれる違反から違反誘導 subgraph を組み立てる。
`Result` は非違反の依存を保持しないため、全依存グラフの描画は対象外とする。
edge 違反は rule と severity を付けた edge label として出力する。
source-only 違反は source node に集約し、stale baseline entry は edge を持たない独立 node として出力する。

JSON report の `violations[].kind` は `forbidden`、`not-in-allowed`、`required`、`reachable`、`unreachable` の 5 値とする。
`not-in-allowed` は allowed 未一致、`required` は必須 import の欠落、`reachable` と `unreachable` はそれぞれ `to.reachable` の `true` と `false` に対応する。
orphan、packageName、numberOfDependents、folder scope の違反は forbidden ルールから生成されるため、kind は `forbidden` になる。

CLI の既定動作は検証であり、`--config FILE` を必須、`--scan-root DIR` を `.`、
`--output-type err|json|mermaid|dot` を `err` の既定値とする。scan に使う module file は
`<scan-root>/go.mod` に固定し、`--baseline FILE` が指定された場合だけ baseline を適用する。
`--generate-baseline` は既存 baseline を適用する前の全 current violation から canonical JSON を
stdout に書き、違反の severity にかかわらず生成成功を exit 0 とする。
検証の exit code は未抑止の error severity 数 + stale entry 数を 255 上限で返し、usage・設定・
scan・出力失敗は exit 2 とする。上限により Unix 系で 256 の倍数が process status 0 に切り詰め
られることを防ぎ、検証失敗は常に non-zero を維持する。

`go install github.com/butaosuinu/godep-cruiser@latest` を成立させるため、module root は
共有 CLI runner を呼ぶだけの `package main` とする。`cmd/godep-cruiser` も同じ runner を呼ぶ
互換 entrypoint とし、検証ロジックはどちらの `package main` にも置かない。

### archtest: go test helper (v0.3)

公開 package `github.com/butaosuinu/godep-cruiser/archtest` は、次の単一 API を持つ。

```go
func Check(tb testing.TB, configuration *config.Config, options cruiser.Options)
```

`Check` は最初に `tb.Helper()` を呼び、受け取った `cruiser.Options` を変更せず
`cruiser.Validate` に渡す。helper 専用の Options 型や別の検証経路は作らない。
設定の不正、go.mod からの resolver 初期化、scan、rule 評価、baseline 検証に失敗した場合は
`tb.Fatalf` で停止する。検証を実行できなかった状態を通常 return にすると、依存規則を
検査していないテストが pass に見えるため、ここは fail-closed とする。

検証できた場合は `cruiser.WriteReport` と `cruiser.OutputTypeErr` で全診断を buffer に
まとめる。`result.ErrorCount() > 0`、すなわち未抑止の error severity 違反または stale
baseline entry がある場合は、1 回の `tb.Errorf` でまとめて失敗を報告する。
error を含まない warn / info 診断は 1 回の `tb.Logf` に渡し、テストを失敗させない。
baseline-known 違反は既存 reporter の契約どおり出力しない。

## MVP スコープ(v0.1)

| コンポーネント | v0.1 | 備考 |
|---|---|---|
| scanner(全ファイル parse)+ resolver | IN | stdlib のみで実装 |
| forbidden / allowed(fail-closed)/ orphan | IN | path / pathNot 配列、from グループキャプチャ `$1`、dependencyTypes、severity |
| packageName マッチ + scan root 空回り検出 | IN | Go 固有追加 |
| baseline + 自動失効 | IN | 差別化の本体 |
| reporter: err / json / mermaid | IN | err は修正方法つきメッセージを設計目標にする |
| required ルール / reachable / numberOfDependents 系 | OUT | v0.2 で IN |
| cache | OUT | 10k ファイルの実測は 766.7 ms/op で再検討閾値未満。失効条件が不完全な cache は fail-closed 方針と矛盾するため見送る |
| folder scope | OUT | v0.3 で IN |
| metrics(instability)/ ancestor | OUT | moreUnstable は v0.3 候補 |
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

- v0.1: 上記 MVP(issue ツリーで管理、tag `v0.0.1`)
- v0.2: required + reachable、numberOfDependents 系(tag `v0.2.0`)
- v0.3: `go test` helper、reporter 拡充(dot / HTML)、folder scope、moreUnstable、`internal/engine` と `cruiser` の `ViolationKind` 二重定義の整理(公開 API と JSON の文字列値は維持)

v0.2 以降は、DESIGN の vX.Y と release tag `vX.Y.0` を対応させる。

リポジトリは github.com/butaosuinu/godep-cruiser(public)。

## 参考

- https://github.com/sverweij/dependency-cruiser — v18.0.0(2026-06)、rules-reference / cli ドキュメント
- fanout `docs/arch-test-tools.ja.md` — 既存 Go ツールの不採用決定記録(要求の源泉)
- fanout `internal/arch/arch_test.go` — scanner・stdlib 判定・自動失効セマンティクスの参照原型
- https://github.com/gabotechs/dep-tree — 多言語依存ツール(Go 解析非対応の確認元)
