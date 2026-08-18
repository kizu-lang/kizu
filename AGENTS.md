# AGENTS.md

Kizu はメモリ安全で隠れた制御フローを嫌い、人間がレビュー、記述しやすい高速な開発サイクルが可能な systems programming language です。

## 最優先

基本の実行経路は `kizu run examples/hello.kizu` です。
CLI は `run` / `check` / `test` / `parse` が中核で、`build` / `ir` / `fmt` / `init` /
`cache` がその周りにあります。

言語の正しさは 2 つが持ちます。`examples/` は読んで分かるプログラムと、その出力。
`tests/behavior/` は振る舞いの assert を 1 package にまとめたもので、link も実行も
1 回で済みます。どちらも自分が何を約束するかを**ファイル末尾のコメントブロック**に
書き、conformance test が木を歩いてそれを読みます。書式は `internal/conformance` の
package doc にあります。

リポジトリ全体の構造とデータフローは `docs/architecture.md` を先に読んでください。

設計判断は `docs/principles.md` の原理に照らします。そこで答えが出る判断は
ユーザーに聞かずに進め、答えが出ない判断だけをユーザーに委ねます。
原理を変えられるのはユーザーだけです。決定や実装が原理と矛盾していると
気づいたら、進める前に指摘してください。

## 実装ルール

- シンプルな設計でコンパイラのビルド時間をgoレベルに高速化することを心がける
- `SPEC.md` と矛盾する構文や機能を勝手に追加しない。
- ファイルが 1000 行を超える場合、分割を検討し、関心が分離できていない可能性を疑う。
- ユーザー判断で仕様判断を変える場合だけ `SPEC.md` または `docs/adr/` を更新する。
- `SPEC.md` には今の言語の定義だけを書く。延期・取り下げ・判断の経緯は
  `docs/adr/` に書き、SPEC に「〜は延期します」の類を残さない。
- 実装は Go 一本(ADR-0082)。selfhost は削除済みで、言語が固まるまで作り直さない。

## 禁止事項

- テストを pass させるだけの場当たり的変更やハードコードを入れない。
- LLVM を文字列リテラルで書き下ろさない(ADR-0073)。ソースの形ごとの payload 型・
  関数名分岐・形状 template を作らない(ADR-0081 が示した失敗)。
- 第二実装を作らない。言語機能は Go 実装 1 つで完成させる(ADR-0082)。
- hidden fallback、Go fallback、削除条件のない互換分岐を入れない。
- 関数の内部形状や生成テキスト断片を grep で固定する**構造 pin を新規に追加しない**
  (ADR-0080)。検証は probe 差分・parity manifest・実行 golden で行う。
- `main` へ直接 commit / push しない。

## テストと性能

テスト実行時間は 120s 以内に収めることを目標にしてください。
遅くなったら profile、重複削除、アルゴリズム改善、不要な gate 分離で改善します。
雑な並列化でごまかす改善は NG です。
commit 前は原則 `pre-commit run --all-files` を通してください。
`go test ./...` は pre-push hook にあり、commit 時ではなく push 時に走ります。


## PR Workflow

リファクタは同一PRに含めて問題ない。commitが分かれていればいい
作業は topic branch / Pull Request ベースで進めます。
**commit の前に必ず停止し、ユーザーのコードチェックを受けてください。**
自動での commit / push / merge は、ユーザーがその変更のチェックを終えてから行います。
PR には目的、主要変更、検証結果、対応 Issue を短く書いてください。

## Release

release はユーザー指示があったときだけ、main で `just release <version>`(例: `v0.1.2`)を実行します。
version の source は git tag のみで、tag push を受けて CI が binary を build し Release に添付します。
