# Kizu Decision Criteria

Kizu の構文、型規則、stdlib 形状、compiler behavior を変更するときの判断基準です。

## 言語思想

Kizu は systems programming language を目指します。
制御性と監査しやすさが増す場面では Zig 的な明示性を選びます。
ただし safe Kizu は Rust safe code 相当のメモリ安全性を守ります。

中核の標語:

```text
傷を作らない。傷を隠さない。
```

実務上の意味:

- safe Kizu に隠れた memory hazard を作らない。
- cost、allocation、blocking I/O、concurrency、dynamic dispatch を隠さない。
- target / build cache 相当の生成物を無制限に増やさない。
- CI の必須 path を重くしない。
- default value や silent fallback で failure を隠さない。
- 既知の design debt を曖昧な TODO として残さない。

## 判断の階段

複数の設計案がある場合は、上から順に満たすものを選びます。

1. safe Kizu が memory-safe のままである。
2. ownership、borrow、cleanup、concurrency boundary を静的に検査できる。
3. failure mode が明示され、diagnostic を失わない。
4. runtime cost と allocation が見える。
5. build time、cache size、CI 実行時間が測定可能で、悪化を検知できる。
6. compiler が複雑な special case なしで実装できる。
7. 構文が読みやすく一貫している。
8. 実際の反復 workflow が楽になる。

## 明示性ルール

次は明示的に見える形にします。

- allocation
- deallocation / `deinit`
- I/O capability
- task group / thread scope
- raw pointer operation
- C ABI conversion
- dynamic dispatch
- error propagation

次を必要とする設計は避けます。

- global allocator
- global async runtime
- implicit string allocation
- implicit C string conversion
- implicit blocking I/O
- best-effort fallback

## Build / Cache / CI

compiler、backend、stdlib、test、CI に関わる変更では、次を確認します。

- no-op rebuild が不必要に重くならない。
- single-file edit rebuild の再処理範囲が説明できる。
- cache key、保存条件、prune 条件、status 表示が明確である。
- debug artifact や巨大な中間生成物は opt-in で保存する。
- CI の必須 path に巨大 artifact 生成や無制限 cache population を入れない。
- 重い performance 測定は `just perf` / `just perf-cache` のような明示 command に分ける。
- performance regression は GitHub issue の acceptance criteria で扱える形にする。

## Absence と Failure

`?T` は absence が想定内で、diagnostic が重要でない場合だけ使います。

- lookup miss
- search miss
- optional configuration value

`!T` は invalid input または recoverable failure に使います。

- out-of-bounds slice range
- allocation failure
- I/O failure
- message 付き parse failure
- validation を要求した場合の invalid UTF-8

trap は compiler bug または不可能な内部状態だけに使います。

## Zero-Debt な仕様変更

言語または stdlib を変更する場合:

- 同じ変更で SPEC または ADR を更新する。
- work scope が変わるなら GitHub issue の acceptance criteria を更新する。
- user-visible behavior が変わるなら positive / negative example を追加する。
- parser / checker / interpreter behavior が変わるなら conformance test を追加する。
- 明示要件がない限り、旧構文の互換維持ではなく削除を選ぶ。
