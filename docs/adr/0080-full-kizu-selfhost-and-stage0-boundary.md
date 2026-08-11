# ADR-0080: selfhost はフル Kizu で書き、bootstrap の正は Go backend(stage0)に置く

## Status

Accepted.

## Context

selfhost の開発速度が構造的に落ちていた。2026-08-11 の実作業で原因を実測した結果、
個別のバグではなく、次の二重束縛が本体だと確認された。

selfhost のソースは「selfhost backend 自身がコンパイルできる範囲(subset)」に収めて
書く運用になっていた。backend は AST→SSA の一般変換を 1 つ持つ代わりに、ソースの形
ごとの lowering(loop shapes、value-cursor latch、関数名決め打ちの template 群)を
積み上げて成長してきたため、subset は狭く、かつ形の認識が古くなると黙って意味論を
落とす。実害として同日に 3 件を修正している:

- 関数名決め打ち template が成長後のソースの意味論を黙って捨て、hosted parser が
  `identity<i64>(7)` を比較式として誤 parse(PR #1492)
- while latch call の引数が body 最終 alias でなくループ先頭の値を読み、
  `a::b` を含む spelling で hosted `check` が無限ループ(PR #1491)
- comptime 実装(#1472)の作業配分が、言語機能 2 割・subset 適合と miscompile 追跡と
  pin 更新 8 割に転倒

さらにフィードバックループも壊れていた。CI が無く全 gate が 1 台の開発機の環境に
依存していたため、toolchain ノイズで bootstrap gate が数週間 red のまま気づかれず、
その間の backend 退行は未検証で積もった。gate が 21 個 red のままの WIP checkpoint
(1abebb90、156 files)が main に入った例もある。検証の多くが「関数の内部形状を
grep で固定する構造 pin」だったため、リファクタのたびに pin の考古学が発生していた
(PR #1488 の分析では 21 失敗中 15 が stale pin)。

他言語の bootstrap 戦略との比較: Zig は bootstrap 用実装(C++)を完成まで保持し、
self-hosted compiler をフル Zig で書いた(subset 方言を作らなかった)。Rust の
stage0 は常に前リリースのバイナリで、コンパイラは常にフル言語で書ける。Go は C
実装を機械翻訳で一括移行した。subset で二重束縛を作った言語は見当たらない。

Kizu には完動する stage0 が既にある: Go backend は selfhost 全体(約 16 万行)を
約 95 秒で native コンパイルできる。

## Decision

1. **selfhost のソースはフル Kizu で書く。** 必須要件は「Go backend(stage0)で
   コンパイルと検査が通ること」であり、selfhost backend の subset に合わせて
   ソースを書き下げることはしない。
2. **selfhost backend の自己コンパイル(stage/stage1/stage2 比較)は
   flip-readiness gate に位置づけを変える。** subset 未対応による red は開発を
   block しない。未対応は `docs/selfhost-backend-generalization.md` に gap として
   記録し、backend の一般化 backlog にする(frontend ソースの書き直しでは吸収しない)。
3. **backend は一般化でのみ成長させる。** 新しい形状 lowering・関数名分岐の追加を
   禁止し、既存 shapes は一般 lowering(environment ベースの SSA 構築)への置換で
   退役させる。退役数を進捗指標として同ドキュメントで数える。
4. **検証は挙動で行う。** 関数の内部形状や生成テキスト断片を固定する構造 pin の
   新規追加を禁止し、probe 差分・parity manifest・実行 golden に寄せる。
5. **main は green のみ。** red な gate を含む変更は merge しない。WIP checkpoint
   は branch に置く。最小の CI(push ごとの daily suite、nightly の bootstrap +
   parity)でこれを機械的に守る。

## Consequences

- 言語機能の開発は「Go 実装 + selfhost frontend(フル Kizu)」のペースに戻る。
  backend の一般化はそれと並行する独立トラックになり、互いを block しない。
- flip(selfhost backend への切替)の判定条件は「一般 lowering が selfhost corpus
  全体をコンパイルできること」に明確化される。個々の shape の温存は flip を
  近づけない。
- bootstrap gate の意味が変わるため docs/selfhost-test-tiers.md を改訂する。
  nightly CI で bootstrap が red になった場合、それが「記録済み gap」か「退行」かを
  generalization ドキュメントの記録で判別する。
- AGENTS.md の禁止事項にこの決定の運用形(shape 追加禁止・構造 pin 新規禁止・
  main green)を反映する。
