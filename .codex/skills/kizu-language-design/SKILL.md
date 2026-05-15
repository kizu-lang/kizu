---
name: kizu-language-design
description: Kizu language/compiler/stdlib design skill. Use for Kizu memory safety, ownership/borrow, allocator, async/concurrency, ADR, GitHub issue, examples, conformance tests, self-host compiler, std namespace, error union vs optional, build/cache/CI performance, and zero-debt specification decisions.
---

# Kizu Language Design

Kizu の設計・実装を、プロジェクト思想に沿わせるための skill です。
Kizu は Zig 的な明示性、Rust の safe code 相当のメモリ安全性、隠れた runtime 挙動を
持たない compiler-friendly な仕組みを重視します。

## 設計優先順位

要求が衝突した場合は、この順で判断します。

1. safe Kizu のメモリ安全性
2. ownership、lifetime、cleanup boundary の明確さ
3. 失敗と capability の明示性
4. performance、build time、cache size の予測可能性
5. 構文と compiler 実装の単純さ
6. ergonomics。ただし上記を隠さない場合だけ

削除条件を書かずに compatibility layer、implicit fallback、hidden global、temporary syntax を
追加してはいけません。

## 中核ルール

- safe Kizu では use-after-move、dangling borrow、data race、unchecked bounds access、
  raw pointer escape を許さない。
- `unsafe` は unchecked な低レベル操作を許してよいが、safe API 周辺の ownership、
  move、borrow、concurrency boundary check を黙って無効化してはいけない。
- 言語の魔法より明示的な stdlib capability を優先する。`Io`、allocator、task group、
  channel、mutex、atomic、fs、process API は signature または constructor で見える形にする。
- hidden global runtime、hidden default allocator、implicit blocking I/O、implicit C ABI
  conversion、implicit string allocation を導入しない。
- target / build cache 相当の生成物が無制限に肥大化する設計を避ける。
- compiler、backend、stdlib、test、CI に関わる変更では、build time、cache size、
  no-op rebuild、CI 実行時間への影響を常に確認する。
- examples と conformance cases は仕様の一部として扱う。
- active work は GitHub Issues で管理し、長く残る設計判断は ADR に残す。

## 構文と命名

- namespace lookup は `::` を使い、`.` は使わない。
- field access と method call は `.` を使う。
- primitive type は lowercase にし、整数幅が重要な場合は `i64`、`u8` のように明示する。
- 曖昧な `int` や primitive `string` を復活させない。
- string literal は `[]const u8` として扱う。
- owned stdlib type は module-qualified PascalCase にする。

```text
std::array::Array<T>
std::string::String
std::map::Map<K, V>
std::set::Set<T>
std::atomic::Atomic<T>
std::sync::Mutex<T>
std::channel::Channel<T>
```

## Error と Optional の判断

この基準で選びます。

```text
?T    値がないことが正常系
!T    invalid input、I/O failure、allocation failure、理由を持つ recoverable failure
trap  compiler bug または回復不能な内部 invariant violation
```

例:

```text
std::mem::index_of(bytes, needle) -> ?usize
std::mem::slice(bytes, start, end) -> ![]const u8
std::fs::read_file(io, path) -> ![]const u8
```

invalid range や diagnostic が必要な失敗に `?T` を使ってはいけません。
将来 ADR で変更しない限り、Rust 風の `Result<T, E>` は導入しません。

## v0.2 Stdlib と Self-Host

v0.2 stdlib の API は、Kizu self-host compiler から見て必要性を確認します。

- `std::mem`: source buffer scan、byte comparison、safe slicing
- `std::array::Array<T>`: token list、AST child list
- `std::string::String`: diagnostic、owned text construction
- `std::map::Map<K, V>`: symbol table、scope
- `std::fs` / `std::path`: source loading、artifact path
- `std::io` / `std::process`: CLI args、stdout、stderr、exit code
- `std::testing`: compiler component test、conformance reuse

self-host 作業で不足 API が見つかった場合は、self-host skeleton だけに TODO を残さず、
対応する stdlib issue に反映します。

## References

設計 tradeoff を判断・review するときは `references/decision-criteria.md` を読みます。
v0.2 stdlib または self-host compiler skeleton を扱うときは
`references/stdlib-selfhost.md` を読みます。
