# AGENTS.md

Kizu はメモリ安全で隠れた制御フローを嫌い、人間がレビュー、記述しやすい高速な開発サイクルが可能な systems programming language です。

## 最優先

基本の実行経路は `kizu run examples/hello.kizu` です。
CLI は `run` / `check` / `test` / `parse` が中核で、`build` / `ir` / `fmt` / `lint` / `init` /
`cache` がその周りにあります。

言語の正しさは実装とテストが持ちます。コンパイラ本体(`compiler/`)、標準ライブラリ
(`lib/kizu/std/`)、`examples/`、`tests/behavior/`、`kizu lint`、`docs/principles.md` が
唯一の正本(Single Source of Truth)です。`examples/` は読んで分かるプログラムとその出力、
`tests/behavior/` は振る舞いの assert を 1 package にまとめたもので、link も実行も
1 回で済みます。どちらも自分が何を約束するかを**ファイル末尾のコメントブロック**に
書き、conformance test が木を歩いてそれを読みます。書式は
`compiler/tests/harness/src/conformance/conformance.kizu` の冒頭にあります。

設計判断は `docs/principles.md` の原理に照らします。そこで答えが出る判断は
ユーザーに聞かずに進め、答えが出ない判断だけをユーザーに委ねます。
原理を変えられるのはユーザーだけです。決定や実装が原理と矛盾していると
気づいたら、進める前に指摘してください。

## 実装ルール

- シンプルな設計でコンパイラのビルド時間をgoレベルに高速化できる設計とする
- 既存の構文・型規則・所有権規則と矛盾する機能を勝手に追加しない。仕様や設計意図の
  正本は `docs/principles.md`、コンパイラ実装、`lib/kizu/std/` の型と `///` doc comment、
  `examples/`、`tests/behavior/` とし、実装と重複する散文ドキュメント(仕様書・ADR・API 文書)を
  新設しない。変更の理由や背景は commit message に書く。
- shipping する binary は `compiler/` から build した Kizu compiler です。それを
  build する経路は `scripts/seed.txt` に pin した前リリース(種)から始まる 1 本
  だけです。種が `compiler/` を build して stage1 を作り、
  stage1 が check / test を回す。`compiler/` の check / test / build は
  `just selfhost`(= `scripts/bootstrap.sh`)から回す。`kizu version` が名乗る行は
  commit ごとに変わるので checked-in にできず、`scripts/gen-selfhost-version.sh` が
  先に生成する必要がある(`scripts/bootstrap.sh` は自分で走らせる)。
  commit hook は種と stage1 の check までで、test は push 時と CI に回る。
- コンパイラの挙動を変える変更は、言語機能の追加でなくバグ修正でも、
  `just selfhost` が全部通ること(種と stage1 の check / test、test harness、
  stage2 の self-rebuild、`scripts/check-sources.sh`)で完了とする。
- `compiler/` は `scripts/seed.txt` に pin した前リリース(種)が、その std ごと
  build できる範囲に保つ。種の知らない言語機能や std API を `compiler/` で使う
  変更は 2 リリースがかりにする(1 回目のリリースでコンパイラに教え、種を更新した
  次のリリースで `compiler/` から使う)。確認は `just selfhost`。

## コメント

`compiler/src` と `lib/kizu/std` のコードには説明コメントを書きません。
コードが何をするかは名前で、何を約束するかはテストと `assert` と型で分かるようにします。
なぜそう書いたかの理由・背景は commit message に書きます。

- 言語公式の `kizu lint` がコメント規約を機械強制します。通常の `//` コメント(`// SAFETY:` や行末コメントを含む)はすべて禁止し、許可するのは `internal` ではない module の外部公開インターフェース(`pub` 宣言、`export "<abi>" fn`、公開型の公開メンバー、公開型を receiver とする method)の直前に空行なしで付いた `///` doc comment ブロックだけです。
- コンパイラは `unsafe` 文の `// SAFETY:` や `unsafe fn` / `unsafe struct` の `///` を必須にしません。ただし `scripts/seed.txt` に pin した種が旧リリースの間は、`compiler/src` 内の既存の `// SAFETY:` と `unsafe` 向け `///` を種更新後の次段階で削除します。
- `examples/` や `tests/` の末尾ブロック、corpus の期待値のように機械が読む
  コメント形の行はコメントではなくテストデータなので、この規則の外です。
- pre-commit hook `no-code-comments`(`scripts/comment-lint/check.py`、検査は
  同じ場所の `test.py`)は、種の更新と `lib/kizu/std/src` の既存 `//` から `///` への移行が終わるまで暫定的に残しています。移行が完了したら `kizu lint` へ置き換えます。

## 禁止事項

- テストを pass させるだけの場当たり的変更やハードコードを入れない。
- LLVM を文字列リテラルで書き下ろさない。ソースの形ごとの payload 型・
  関数名分岐・形状 template を作らない(`docs/principles.md` §7)。
- 利用者が通る経路を 2 つにしない。Go を fallback にも feature flag にも戻さない。
- hidden fallback、Go fallback、削除条件のない互換分岐を入れない。
- 関数の内部形状や生成テキスト断片を grep で固定する**構造 pin を新規に追加しない**。
  検証は `examples/` と `tests/behavior/` の実行結果で行う。
- `main` へ直接 commit / push しない。

## テストと性能

テスト実行時間は 30s 以内に収めることを目標にしてください。
遅くなったら profile、重複削除、アルゴリズム改善、不要な gate 分離で改善します。
雑な並列化でごまかす改善は NG です。
commit 前は原則 `pre-commit run --all-files` を通してください(数秒)。
`scripts/bootstrap.sh`(`just selfhost`)のフル版は pre-push hook にあり、commit 時
ではなく push 時に走ります。Kizu compiler 自身の test(`kizu test compiler`)も
そこに含まれます。

## CI

`difftest-nightly` が毎晩、direct backend と LLVM の出力を比べる。
食い違いは `difftest-nightly` ラベルの issue になる。

- 実行結果: `gh run list --workflow difftest-nightly.yml`
- 見つかったバグ: `gh issue list --label difftest-nightly`
- 再現: `scripts/difftest-generated.sh target/seed/stage1/bin/kizu <seed>`

## 形式仕様

`spec/` の Lean、TLA+、Quint は `just selfhost` でも CI でも動かしません。CI を長くしないためで、
対応する箇所を変えた人が手元で回します。形は `spec/README.md` にあります。

| 変えたもの | 流すもの |
| --- | --- |
| `std::tls` の状態機械 | `just spec-table`(生成と TLC の検査)、`just spec-trace`(Lean) |
| `spec/tls_*_properties.tla` | `just spec-verify` |
| `spec/*.lean` | `just spec-trace` |

`std::tls` を変えて生成物が古いままなら `just selfhost`(`scripts/check-sources.sh`)が落ちます。生成された範囲と
`spec/tls_client.tla`、`spec/tls_server.tla` は手で書き換えず、検査を通すために性質を弱めません。

## PR Workflow

リファクタは同一PRに含めて問題ない。commitが分かれていればいい
作業は topic branch / Pull Request ベースで進めます。
PR には目的、主要変更、検証結果、対応 Issue を短く書いてください。

## Release

release はユーザー指示があったときだけ、main で `just release <version>`(例: `v0.1.2`)を実行します。
version の source は git tag のみで、tag push を受けて CI が binary を build し Release に添付します。
