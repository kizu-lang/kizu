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
std::testing::expect(condition: bool) -> void
std::testing::fail(message: []u8) -> !void
```

`expect` は assertion ごとの `try` を不要にするため void を返す。
condition failure は `std::builtin::test_fail(message: []u8) -> void`
経由で runtime error として停止し、runner が readable な failure message を表示する。
Go 側の責務は、この明示 trap primitive と runner の未処理 runtime error 表示境界に限定する。

`fail` は caller-provided byte message を通常の `error(...)` に渡す `!void`
helper のまま残す。unreachable match arm など、呼び出し側の error-union 経路へ
明示的に戻したい場合は `return std::testing::fail("...");` を使う。

Generics 不在のまま `expect_equal_<type>` family を増やすと API と diagnostic 実装が
型ごとに爆発するため、v0.2 の public testing API には含めない。
値の比較は `expect(left == right)`、byte slice は
`expect(std::mem::equal_bytes(left, right))` のように caller 側で明示する。
必要なら caller が `std::fmt` と `fail` で domain-specific diagnostic を構築する。

`kizu test <file>` は v0.2 では discovery なしの single-file runner とする。
file を check して `main` を実行し、未処理 error がなければ `test: ok` を表示する。

## Consequences

- self-host compiler component tests を Kizu source として書き始められる。
- assertion の大半から `try` が消え、テストコードの signal/noise が下がる。
- generics なしで typed equality helper を増やす圧力を避けられる。
- expected / actual の rich diagnostic は generic equality か message builder helper の
  設計後に再導入する。
- `expect` failure は通常の recoverable error ではなく、明示 test trap になる。
- test discovery、location-aware diagnostics は後続に残す。
