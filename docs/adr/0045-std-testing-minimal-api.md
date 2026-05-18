# ADR 0045: std::testing minimal API

Status: 採用

## Context

v0.3 self-host compiler へ進むには、Kizu source 側で lexer、parser、
checker component を小さくテストできる足場が必要になる。

Go の test suite だけに依存すると、self-host compiler へ移行するときに
同じ corpus と assertion pattern を再利用しにくい。

## Decision

v0.2 で `std::testing` の最小 API を Kizu source として実装する。

```text
std::testing::expect(condition: bool) -> !void
std::testing::expect_equal_i64(expected: i64, actual: i64) -> !void
std::testing::expect_equal_bool(expected: bool, actual: bool) -> !void
std::testing::expect_equal_bytes(expected: []const u8, actual: []const u8) -> !void
std::testing::fail(message: []const u8) -> !void
```

Assertion failure は panic ではなく `!void` の error として返す。
test source は `try` で失敗を伝播し、runner が readable な failure message を表示する。

`expect` と `fail` は fixed message または caller-provided byte message を
`error(...)` に渡す。Equality helpers は `std::mem` で比較し、失敗時だけ
明示 allocator-backed `std::string::String` を作り、`std::fmt` で deterministic な
expected / actual diagnostic を構築して `error(...)` に渡す。

`error(...)` は message bytes を error payload に copy して所有するため、
`std::testing` は local `String.as_bytes()` view を返さない。Go 側の責務は
`kizu test` runner、error payload の所有境界、unhandled error 表示境界に限定し、
`std::builtin::testing_*` は持たない。

`kizu test <file>` は v0.2 では discovery なしの single-file runner とする。
file を check して `main` を実行し、未処理 error がなければ `test: ok` を表示する。

## Consequences

- self-host compiler component tests を Kizu source として書き始められる。
- expected / actual の順序を固定できる。
- assertion diagnostics can evolve in Kizu with `std::fmt` instead of growing
  Go-backed stdlib behavior.
- generic equality、test discovery、location-aware diagnostics は後続に残す。
- failure は通常の error-union 経路を通るため、例外や hidden runtime は不要。
