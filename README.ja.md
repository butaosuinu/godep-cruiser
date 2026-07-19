# godep-cruiser

[English](README.md) | [日本語](README.ja.md)

Go ソースツリーの依存ルールを検証します。

godep-cruiser は、Sander Verweij 氏による [dependency-cruiser](https://github.com/sverweij/dependency-cruiser) の概念をクリーンルームで Go 向けに再実装したものです。
正規表現による `path` / `pathNot` のマッチングを備えた forbidden / allowed / required ルール、推移到達、package の fan-in と不安定度の検査、依存種別（stdlib / module 内 / third-party / 解決不能）の分類、および違反 baseline を提供します。
さらに、本家にはない機能として、違反が解消されて対応する baseline エントリが未使用になると検証を失敗させるため、従来から許容していた例外は自動的に失効します。

dependency-cruiser のコードは一切翻訳しておらず、設計は公開ドキュメントと観察可能な挙動に基づいています。
本家プロジェクトとは提携していません。

## クイックスタート

モジュールルートのパッケージとしてコマンドをインストールします。

```sh
go install github.com/butaosuinu/godep-cruiser@latest
```

ルール設定を `godep-cruiser.json` として保存し（[設定](#設定)にある完全な例はそのまま有効です）、現在のモジュールを検証します。

```sh
godep-cruiser --config godep-cruiser.json --scan-root .
```

既定の出力は人間が読める `err` 形式です。
JSON、Mermaid、GraphViz DOT、および自己完結型 HTML は明示的に選択します。

```sh
godep-cruiser --config godep-cruiser.json --scan-root . --output-type json
godep-cruiser --config godep-cruiser.json --scan-root . --output-type mermaid
godep-cruiser --config godep-cruiser.json --scan-root . --output-type dot
godep-cruiser --config godep-cruiser.json --scan-root . --output-type html > report.html
```

検証結果には違反していない依存が含まれないため、Mermaid と DOT は違反から誘導される部分グラフだけを可視化します。
edge 違反はラベル付き edge として描画され、source-only 違反は強調表示された source node に集約され、stale baseline エントリは独立した強調表示 node になります。

HTML レポートは、インライン CSS のみを使用し、JavaScript や外部アセットを含まない、直接開ける単一ページです。
severity 別の要約、違反テーブル、stale baseline エントリを含みます。

JSON レポートの `violations[].kind` は次のいずれかです。

- `forbidden`：folder scope の package edge、および source-only の orphan、package-name、dependent-count 検査を含む forbidden ルールへの一致
- `not-in-allowed`：どの allowed ルールにも一致しない依存
- `required`：必須 import を欠く source file
- `reachable`：一致する package が推移的に到達可能
- `unreachable`：一致する package が entry point の closure 外にある

JSON の source-only 違反では `to` が `null` となり、edge 違反では対象の依存または package を含みます。

完全一致 baseline を生成し、適用します。

```sh
godep-cruiser --config godep-cruiser.json --scan-root . \
  --generate-baseline > godep-cruiser-baseline.json

godep-cruiser --config godep-cruiser.json --scan-root . \
  --baseline godep-cruiser-baseline.json
```

検証の exit code は、抑止されていない `error` 違反数と stale baseline エントリ数の合計であり、失敗した検証がプロセスの終了ステータスとして必ず 0 以外になるよう上限を 255 とします。
`warn` と `info` の違反は引き続き報告しますが、コマンドを失敗させません。
フラグ、設定、スキャン、および出力の失敗は 2、baseline の生成成功は 0 で終了します。

## 背景

Go コンパイラは import cycle を禁止しますが、layer の方向、core package の stdlib 純粋性、依存を持ってはならない tools ツリーなどのアーキテクチャ上の制約は検査しません。
既存の Go ツールは、この領域の要件を部分的にしか満たしません（stdlib の制限がない、file 単位の例外がない、fail-closed の分類がない、自動失効する例外がない、など）。
godep-cruiser は、dependency-cruiser で実証されたルールモデルを用いて、この不足を埋めます。

## 設定

v0.3 の設定形式は JSON のみとし、ランタイムを標準ライブラリだけで構成できるようにしています。
公開されている [JSON Schema](schema/godep-cruiser.schema.json) は受理するすべてのフィールドを記述し、loader は Go の正規表現、数値 capture 参照、未知のフィールド、および入力位置も検証します。

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

`from.path` のキャプチャグループは、`to.path` と `to.pathNot` で `$1`、`$2`、およびそれ以降の数値参照として使用できます。
マッチングと検証のセマンティクスは [DESIGN.ja.md](DESIGN.ja.md#設定形式と-loader) を参照してください。

forbidden ルールの省略可能な `scope` は、既定で `module` です。
既定の scope は個々の source file と import に一致します。
一方、`scope: "folder"` は、`from.path` と `to.path` をモジュールルート相対の package path（`.` はモジュールルート）に対して照合し、重複を除いた local package edge ごとに 1 件の違反を報告します。
folder scope の違反は、違反の `from.path` に import 元 package path、line `0`、空の package name を持ち、import 先は raw import path のない local package です。
folder scope を使用できるのは forbidden ルールだけです。
`dependencyTypes`、`dependencyTypesNot`、`from.orphan`、`from.packageName`、`to.reachable`、および `to.reachableFilePathNot` を拒否しますが、`from` の dependent-count 条件は引き続き使用でき、`from.path` の capture 参照も `to` で使用できます。

各 `required` ルールは、`from` に一致するすべての file を検査し、その file の import のいずれも `to` に一致しない場合、source-only 違反を 1 件報告します。
したがって、import を一つも持たない一致 file もルールに違反します。
`from: {}` は catch-all です。
required ルールでは `to: {}` と `from.orphan` は無効です。

forbidden ルールでは `to.reachable` を設定して、local package graph を評価できます。
`true` は、一致する file の local import から到達可能な、一致する各 target package を報告し、diagnostic の line は到達経路を開始する import を示します。
`false` は、一致する file を含む package を entry point として扱い、その推移 closure 外にある一致する target package 内のすべての file を報告します。
どちらの形式も `to.path` が必須で、`to.pathNot` を使用でき、dependency type のフィールドを拒否します。
capture 参照は `true` では引き続き使用できますが、`false` では無効です。
allowed と required ルールでは `reachable` を使用できません。

`to.reachableFilePathNot` は、推移 local-package edge を構成する file のスキャンルート相対かつ slash 区切りの path に基づいて、その edge を任意に除外します。
これは `to.reachable` と組み合わせる場合だけ使用できる、空でない正規表現配列であり、`from.path` の capture は展開しません。
このフィールドは opt-in であり、省略すると `_test.go` file だけで構成される edge を含むすべての edge を保持します。
production file と除外対象 file の両方が同じ package edge を構成する場合、その edge は引き続き辿ることができ、構成するすべての file が除外された場合にだけ除去されます。

`reachable: true` では、filter は一致する各 source file の開始 import 自体には適用せず、その先の推移 edge から適用するため、seed file の除外には引き続き `from.pathNot` を使用します。
`reachable: false` では、seed package から辿るすべての edge に適用されます。
filter が変更するのは closure の構成だけであり、違反の shape と baseline identity は変わりません。

`from.numberOfDependentsLessThan` と `from.numberOfDependentsMoreThan` は、その source package を直接 import する重複のない local package 数を、厳密な `<` と `>` の境界で比較します。
module scope の forbidden ルールでは、どちらかの条件を `to: {}` と組み合わせると、一致する file ごとに source-only 違反を 1 件報告します。
folder scope では、`to: {}` はすべての outgoing local package edge に一致するため、この条件は source package を filter し、import のない package は違反を生成しません。
required ルールでは dependent-count 条件を使用できません。

forbidden ルールでは `to.moreUnstable: true` を設定し、import 先 package が import 元 package より厳密に不安定な場合だけ local edge に一致させることができます。
package の不安定度は、self-edge を除いた重複のない local package edge に対する `FanOut / (FanIn + FanOut)` であり、分母が 0 の場合は `0` と定義します。
不安定度が等しい場合は一致せず、stdlib、third-party、および解決不能な依存も一致しません。
このフィールドは module scope と folder scope の両方で使用できますが、`false`、allowed または required ルール、`reachable`、`local` を含まない `dependencyTypes` リスト、および `local` を含む `dependencyTypesNot` リストは拒否されます。

## ライブラリ API

公開 facade は CLI とは独立して import できます。
設定の読み込み、scan、検証、任意の baseline 適用、report、および error count は、通常の Go 関数呼び出しとして使用できます。

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

上のコード例では `github.com/butaosuinu/godep-cruiser/config` と `github.com/butaosuinu/godep-cruiser/cruiser` を import します。
module file が `<ScanRoot>/go.mod` ではない場合は `Options.GoModPath` を設定してください。
`go.work` と nested module の自動探索は行いません。

### Go test ヘルパー

依存検証は通常の Go test として実行できます。
モジュールルートに次のような test を配置します。

```go
func TestArchitecture(t *testing.T) {
	configuration, err := config.LoadFile("godep-cruiser.json")
	if err != nil {
		t.Fatal(err)
	}

	archtest.Check(t, configuration, cruiser.Options{ScanRoot: "."})
}
```

`config` と `cruiser` に加えて、`github.com/butaosuinu/godep-cruiser/archtest` を import します。
`error` severity の違反と stale baseline エントリは test を失敗させます。
`warn` または `info` の違反だけを含む実行は、test を失敗させずに log へ記録されます。
`archtest.Check` 内で設定の検証または scan に失敗すると、`Fatalf` で test を停止します。

## baseline

baseline は、完全一致する違反キーを含む厳密な JSON 文書です。

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

通常の import edge のキーは `rule` + `from` + `to` であり、`to` は解決済み path ではなく Go source に記述された raw import path です。
`reachable: true` の違反には単一の raw target import がないため、その `to` キーはモジュールルート相対の target package path です。
folder scope の package edge も同様に、`from` と `to` の両方にモジュールルート相対の package path を使用します。
module scope の orphan、package-name、dependent-count 検査、required ルール、および `reachable: false` ルールなどの source-only 違反は `to` を省略し、`rule` と `from` の組で一致します。

baseline には 3 つの結果があります。

- 現在の違反が baseline にない場合、設定された severity で報告され、baseline によって severity が上がることはありません。
- baseline に現在の違反と一致するエントリがある場合、既知の違反として抑止されます。
- いずれの現在の違反にも一致しない baseline エントリは常に stale error になり、その診断は baseline からエントリを削除するよう利用者に指示します。

生成されるエントリはソートされ、重複が除去されます。
読み込み時には未知のフィールド、空のキー、重複キー、および JSON 末尾の余分な内容を拒否しますが、stale エントリが削除済みの source を指す場合があるため、参照される file または import が現在も存在することは要求しません。
正規表現エントリ、`//nolint` directive、および日付に基づく有効期限はサポートしません。
完全一致キーによって stale 検出が決定的になり、対応する違反が解消されるとエントリが失効します。

## ライセンス

[MIT](LICENSE)
