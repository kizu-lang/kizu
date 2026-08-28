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

- シンプルな設計でコンパイラのビルド時間をgoレベルに高速化できる設計とする
- `SPEC.md` と矛盾する構文や機能を勝手に追加しない。
- ユーザー判断で仕様判断を変える場合だけ `SPEC.md` または `docs/adr/` を更新する。
- `SPEC.md` には今の言語の定義だけを書く。延期・取り下げ・却下した案は
  adr に書き、SPEC に「〜は延期します」の類を残さない。
- adrを気軽に追加するな。adrの追加を考える前に、コメントやコミットコメントで済む話かどうかを検討する。追加する場合も短く保つ。理想は50行以内。
- std の API は `docs/std/` に書く。SPEC が std について持つのは compiler が
  知っている契約だけ —— capability としての `Allocator`、storage 型に対する
  borrow / ownership の検査規則、`test` 宣言。境界は「利用者が自分で同じものを
  書けるか」で決まる。`std::json` は書けるので `docs/std/`、`Array.at` が
  capture 条件でしか消費できないのは書けないので SPEC。
- shipping する binary は `compiler/` から build した Kizu compiler です。Go の
  `internal/` + `cmd/kizu` はそれを build する seed であり、両実装の出力を
  突き合わせる oracle でもあります(ADR-0130)。片方だけに言語機能を足さない。
  `compiler/` の check / test / build は `just selfhost` から回す。`kizu version` が
  名乗る行は commit ごとに変わるので checked-in にできず、
  `go run ./scripts/gen-selfhost-version` が先に生成する必要がある(pre-commit と
  `cmd/kizu` の test は自分で走らせる)。
- Go の comment は英語で書く。package comment と `package main` の command
  comment は必須(pre-commit の `go comments` が見ている)。

## ADR

ADR が持つのは **なぜそうしたか** と **却下した案とその理由** の 2 つです。
現在の仕様は `SPEC.md`、進行中の作業は GitHub Issues が持ちます。

- **決定が変わったら、その ADR を書き換える。** 前の判断を訂正・置換する
  だけの ADR を新しく作らない。読む人が最新の判断に辿り着くまでに 3 本読む
  状態を作らない。変更の経緯は git log が持つ。
- **別の問いなら別の ADR にする。** 前の判断に触れることと、前の判断の
  訂正であることは違う。触れるだけなら独立した ADR を立て、`SPEC.md` から
  見た関係を 1 行で書く。
- **消す。** 次のものは残さない。
  - 消した subsystem や、もう存在しない仕組みの記述
  - 後続の判断に完全に置き換わったもの
  - 規則を別の場所が持っているもの(`SPEC.md`、`docs/` の他の文書、
    pre-commit hook、コードのコメント)
  - 進行管理・phase 計画・release gate の類
- **却下案とその理由があるものは残す。** ADR の主な価値は同じ検討を 2 回
  しないことなので、決定だけなら SPEC にあっても却下表は ADR にしか無い。
- **消すときは参照も畳む。** 参照していた側から番号を消し、必要な内容は
  その場に直接書く。dangling な `ADR-00xx` を残さない。
- **番号は欠番でよい。** 消した番号は再利用しない。

`docs/adr/README.md` の一覧は、`docs/adr/*.md` の 1 行目から生成できる形に
保ちます。

## 禁止事項

- テストを pass させるだけの場当たり的変更やハードコードを入れない。
- LLVM を文字列リテラルで書き下ろさない。ソースの形ごとの payload 型・
  関数名分岐・形状 template を作らない(`docs/principles.md` §11)。
- 利用者が通る経路を 2 つにしない。Go を fallback にも feature flag にも戻さない。
- hidden fallback、Go fallback、削除条件のない互換分岐を入れない。
- 関数の内部形状や生成テキスト断片を grep で固定する**構造 pin を新規に追加しない**。
  検証は `examples/` と `tests/behavior/` の実行結果で行う。
- `main` へ直接 commit / push しない。

## テストと性能

テスト実行時間は 30s 以内に収めることを目標にしてください。
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
