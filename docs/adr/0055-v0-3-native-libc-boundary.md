# ADR-0055: v0.3 native compiler uses explicit target libc boundary

Status: 採用

## 背景

v0.3 の目標は Kizu-only standalone self-host compiler の完成である。
standalone artifact は native executable とし、LLVM backend を最優先にする。

ただし、libc を言語仕様の前提にすると、freestanding、WASM、embedded、
cross compile、将来の no-libc target で不要な制約になる。
一方で、macOS native executable で raw syscall に寄せると、公開 ABI として
安定している境界を避けることになり、v0.3 の実装負債が大きい。

## 決定

Kizu language core は libc に依存しない。

libc / libSystem は target capability と std backend の実装詳細として扱う。
v0.3 の supported native target は host macOS arm64 に限定し、
その target では `std::os` backend が libSystem 境界を使ってよい。

v0.3 の native path は次の形にする。

```text
Kizu source
  -> Kizu-owned self-host compiler
  -> LLVM IR text (.ll)
  -> llc
  -> object (.o)
  -> lld
  -> native executable
```

target model は、v0.3 で実装する target が限定されていても、
少なくとも次を明示的に保持する。

```text
arch
os
abi
object_format
```

unsupported target は hidden fallback せず、明示的な diagnostic で失敗する。

## libc boundary

libc は Kizu の暗黙 runtime ではない。

許可するもの:

- `std::os` / `std::fs` / `std::io` / `std::process` backend が libc または
  libSystem を使う
- v0.3 macOS native compiler artifact が libSystem に link する
- C ABI boundary を明示した extern declaration

禁止するもの:

- 言語機能そのものを libc 必須にする
- hidden global allocator として libc `malloc` を暗黙利用する
- hidden global I/O runtime を導入する
- target が未対応のときに Go compiler や別 backend へ暗黙 fallback する
- safe Kizu の ownership / borrow / memory-safety rule を libc boundary で弱める

allocator、I/O、process、filesystem は signature または constructor で
capability が見える形にする。

## v0.3 scope

v0.3 で完成させる target:

```text
aarch64-apple-darwin native executable
```

v0.3 で設計上考慮するが、完成条件には含めない target:

```text
x86_64-apple-darwin
x86_64-linux-gnu
x86_64-linux-musl
wasm32-wasi
freestanding
```

## 影響

- Zig に近い形で、libc を言語仕様ではなく target/backend choice にできる
- v0.3 は現実的に macOS native executable を作れる
- 将来 no-libc target を追加しても、言語 core の設計を置き換えずに済む
- target、linker、stdlib backend、cache key の境界が明示される
- self-host compiler の standalone path で Go fallback を残せない
