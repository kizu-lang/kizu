# ADR-0002: Kizu は低レベル寄りのシステムプログラミング言語を目指す

Status: 採用

## 背景

Kizu は Rust の安全性の考え方を参考にしつつ、Rust 互換は目指さない。
ユーザー要望として、Zig に近い明快さと低レベル制御を重視したい。

## 決定

Kizu はシステムプログラミング言語を目指す。

設計では次を優先する。

- 明示的な整数幅
- C ABI との接続
- raw pointer を扱える unsafe 境界
- comptime
- 読みやすい処理系
- macro や build script に依存しない設計
- borrowed view 境界でだけ明示 lifetime annotation を使う設計

## 影響

- 型名は Zig 寄りの小文字 primitive を基本にする
- 将来 `i32` / `u64` / `usize` などを追加する
- C 親和性、WASM / WASI、LLVM backend を後続 Phase として扱う
- 安全性は捨てず、危険な操作は明示境界に閉じ込める
- 長寿命の関係は lifetime parameter ではなく arena / handle で扱う
