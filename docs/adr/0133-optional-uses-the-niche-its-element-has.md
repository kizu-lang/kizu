# ADR-0133: optional は要素が持つ niche を使う

## Status

Accepted.

## Context

`?T` は tag と payload の 2 field で表していました —— `{ i8, T }` を C の規則で
並べたものです。要素が pointer なら alignment 8 なので、1 byte の tag が 8 byte に
広がり、`?Box<T>` は 16 byte になります。

その代償は再帰的なデータ構造そのものです。

| | field | size |
| --- | --- | --- |
| `struct Node { left: ?Box<Node>, right: ?Box<Node> }` | 2 | 32 |
| 同じ節点を C や Zig が書く形(null 可能 pointer 2 本) | 2 | 16 |

ADR-0132 が Box の cell から header を落としても、節点自体が倍のままでした。
binary-trees(節点 1000 万個)の実測は RSS 46MB で、C の 23MB に対して 2 倍です。

`?Box<T>` が tag を必要とする理由は 1 つだけです —— **payload の全 bit 列が
live な値でありうるなら、不在を表す余地が無いから**。Box handle は payload の
address で、borrow は借りた先の address です。どちらも生きている間 null に
なりません。

## Decision

**要素が不在を綴れるなら、optional はその綴りを使う。**

```text
?std::mem::Box<T>   ->  ptr        (null が不在)
?&T / ?&var T       ->  ptr        (null が不在)
その他の ?T          ->  { i8, T }  (従来どおり)
```

`opt.null` は `null`、`opt.some(x)` は `x`、`opt.has(o)` は `icmp ne ptr o, null`、
`opt.value(o)` は `o` です。命令はむしろ減ります —— tag の組み立てが消え、
存在判定は tag を作るのに使っていた比較そのものになります。

`Array.at` / `Map.at` / `Arena.at_mut` が返す borrow optional は、runtime が
返す null 可能 pointer をそのまま渡すだけになります。以前はその pointer から
tag を計算して 2 field に詰め直していました。

### niche を持たない pointer

`Map<K, V>` と `Arena<T>` の handle も pointer ですが、constructor は確保に
失敗すると null を返し、値はそれを持ち歩きます(`std::map::new` は `!Map` では
なく `Map` を返します)。null が live な値なので niche はありません。

`std::arena::Handle<T>` は index であって pointer ではないので、対象外です。

## 却下

| 案 | 却下理由 |
| --- | --- |
| tag を残す | 再帰的な節点が倍のまま。`?Box<T>` が 8 byte で足りるのに 16 byte 払う |
| すべての pointer を niche 扱いにする | `Map` / `Arena` の handle は確保失敗を null で持ち歩く。null が live な値なので、不在と区別できなくなる |
| `Map` / `Arena` の constructor を `!T` にして null を無くし、全 pointer を niche にする | 別の判断。この ADR が答えるのは表現の問題で、失敗の綴りの問題ではない |
| niche を言語に見せる(`?T` の size を綴れるようにする) | 表現は言語の定義ではない。SPEC は `?T` が何を意味するかだけを持つ |
| tag の代わりに payload の別 bit を使う汎用 niche(Rust の niche 最適化) | 今必要なのは pointer の null だけ。汎用の仕組みは、どの型がどの bit を空けているかを型ごとに追う表を要求する |

## Consequences

- `?Box<T>` と `?&T` は 1 word。`Node { left: ?Box<Node>, right: ?Box<Node> }` は
  16 byte になり、binary-trees の RSS は 46MB から 23MB —— C と同値、Rust の
  24MB より小さい。CPU も 1.48s から 1.25s
- 表現は言語から見えない。`?T` の意味も規則も変わらず、SPEC は触っていない
- `zeroinitializer` が引き続き不在を意味する。null pointer は zero bit 列で、
  tag 0 と同じ
- niche を持つ型が増えたら、この 1 か所(`nicheOptionalElem`)に足す
