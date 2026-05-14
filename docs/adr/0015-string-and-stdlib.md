# ADR-0015: string は std 管理にして literal は []const u8 にする

Status: 採用

## 背景

Kizu は低レベル寄りのシステムプログラミング言語を目指す。
高級な owned string を primitive として強く組み込むと、allocator、ownership、
ABI、slice 表現が曖昧になる。

Zig では文字列は主に `[]const u8` として扱われる。

## 決定

文字列 literal は言語が扱う。
v0.1 の source-level type として `string` は採用しない。
文字列 literal は read-only byte slice である `[]const u8` として扱う。

想定:

```text
"hello"     []const u8 literal
[]const u8  read-only byte slice
std.string  将来の owned string
```

Phase 19 では stdlib 基盤を次のように整理する。

```text
[]const u8       v0.1 の string literal type
slice<T>         contiguous mutable view, future
slice<const T>   contiguous read-only view, future
array<T>         owned contiguous collection, future
map<K, V>        hash map, later
set<T>           hash set, later
```

`std.string` と C ABI の間に暗黙変換は置かない。
C へ渡す場合は、将来 `std.string.as_c_string` のような明示 API を使う。

stdlib module naming は lowercase dotted names にする。

```text
std.string
std.io
std.fs
std.mem
std.slice
std.array
```

v0.1 の stdlib 実装は `print` だけでよい。
ただし、今後の API は上の module 境界に寄せる。

## 影響

- Phase 2 interpreter では string literal を `[]const u8` value として扱う
- Phase 3 type checker は `[]const u8` を v0 型として検査する
- allocator を必要とする string 操作は標準ライブラリ側に寄せる
- C ABI では `std.string` を暗黙に `ptr<const u8>` へ変換しない
- collection は `array<T>` を先に検討し、`map` / `set` は後続 phase に回す
- async I/O は stdlib 基盤から切り離して別 phase で扱う
