# Phase 19: stdlib foundation for std.string / slice / collections

状態: 完了

## 目的

Kizu は標準ライブラリを厚めにする方針だが、現状は `print` が中心。

低レベル言語として必要な `std.string` / `slice` / collection の境界を先に固める。

## 方針

- v0.1 の実装は `print` だけでよい
- string literal は v0.1 では `[]const u8` として扱う
- 将来の owned string は `std.string` に寄せる
- `std.string` と C ABI の暗黙変換は禁止する
- collection は `array<T>` を先に検討し、`map` / `set` は後回しにする
- stdlib module は lowercase dotted names にする

## TODO

- [x] `std.string` の所有権と内部表現方針を決める
- [x] `slice<T>` / `slice<const T>` を採用するか決める
- [x] C ABI と std.string / slice の変換を明示化する
- [x] `array<T>` の v0 範囲を決める
- [x] `map` / `set` をいつ扱うか決める
- [x] stdlib module naming を決める
- [x] `std.string` / `std.io` の最小 API を決める

## 受け入れ条件

- [x] `SPEC.md` または ADR に stdlib 基盤方針がある
- [x] std.string / slice / C ABI の境界が明示されている
- [x] v0 で実装する collection と後回しにする collection が分かれている

## module naming

```text
std.string
std.io
std.fs
std.mem
std.slice
std.array
```

## 範囲外

- package manager
- HTTP / JSON など高レベル stdlib
- async I/O
- allocator API の完全設計
