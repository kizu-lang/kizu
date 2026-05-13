# ADR-0015: string は標準ライブラリ管理に寄せる

Status: 採用

## 背景

Kizu は低レベル寄りのシステムプログラミング言語を目指す。
高級な owned string を primitive として強く組み込むと、allocator、ownership、ABI、slice 表現が曖昧になる。

Zig では文字列は主に `[]const u8` として扱われる。

## 決定

文字列リテラルは言語が扱う。
ただし、可変文字列や owned string は標準ライブラリで管理する方針にする。

v0 では実装を単純にするため `string` 型を使ってよい。
ただし、将来は read-only byte slice を土台にする。

想定:

```text
"hello"         string literal
string          v0 の組み込み文字列型
slice<const u8> 将来の低レベル表現候補
std.string      将来の owned string 候補
```

## 影響

- Phase 2 interpreter では `string` を簡易値として扱ってよい
- Phase 3 type checker は `string` を v0 型として検査する
- allocator を必要とする string 操作は標準ライブラリ側に寄せる
- C ABI では `string` を暗黙に `ptr<const u8>` へ変換しない
