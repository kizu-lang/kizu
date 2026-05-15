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
std::string::String  将来の owned string
```

Phase 19 では stdlib 基盤を次のように整理する。

```text
[]const u8       v0.1 の string literal type
slice<T>         contiguous mutable view, future
slice<const T>   contiguous read-only view, future
std::array::Array<T>  owned contiguous collection, future
std::map::Map<K, V>   hash map, later
std::set::Set<T>      hash set, later
```

`std::string::String` と C ABI の間に暗黙変換は置かない。
C へ渡す場合は、将来 `std::string::as_c_string` のような明示 API を使う。

stdlib module naming は lowercase namespace names にし、namespace separator は
ADR-0038 に従って `::` を使う。

```text
std::string
std::io
std::task
std::channel
std::fs
std::mem
std::slice
std::array
```

v0.1 の最小 builtin は `print` とする。
加えて、concurrency / async の安全境界を固めるために、`std::task`、
`std::channel`、`std::thread`、`std::atomic`、`std::sync` の prototype API を
interpreter builtin として扱う。
これらは full stdlib ではなく、memory-safety release gate の対象となる
trusted std prototype とする。

## 影響

- Phase 2 interpreter では string literal を `[]const u8` value として扱う
- Phase 3 type checker は `[]const u8` を v0 型として検査する
- allocator を必要とする string 操作は標準ライブラリ側に寄せる
- C ABI では `std::string::String` を暗黙に `ptr<const u8>` へ変換しない
- collection は `std::array::Array<T>` を先に検討し、`std::map::Map<K, V>` /
  `std::set::Set<T>` は後続 phase に回す
- `Io` は将来 `std::io`、`Task` / `TaskGroup` は `std::task` 境界に寄せる
- async runtime は stdlib API と分けて設計し、safe Kizu の ownership / borrow 制約を維持する
