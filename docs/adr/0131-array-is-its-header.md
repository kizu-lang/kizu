# ADR-0131: `Array<T>` は header そのもので、header への pointer ではない

## Status

Accepted.

## Context

`std::array::Array<T>` は allocator が配った 5 word の header への pointer でした。
つまり **空の array が 1 回 allocation を要求します**。要素を 1 つも持たない
array も、header の分だけ確保してから始まります。

compiler が自分自身を compile するときの実測です。

| 項目 | 数 |
| --- | --- |
| 作られる array | 8,260,540 |
| 一度でも要素を持った array | 5,389,659 |
| header allocation の占める peak memory | 51MB |

差の 287 万本は、確保して、何も入れず、解放しただけの header です。
`Vec<T>` / `ArrayList` を持つ言語はここに allocation を持ちません。値が
header だからです。

## Decision

**`Array<T>` は header そのもの。** `{data, len, cap, allocator}` の 4 word が
値で、`array::new` は `insertvalue` で組み立て、allocator に何も頼みません。
最初の `append` が storage を買います。

header は `elem_size` を持ちません。`sizeof(T)` は compile 時に決まる値で、
compiler はそれを必要とする全ての呼び出しで知っています。header が持つと、
全ての array と、array of array の全要素が、呼び出し側の持っている値を
複製して運ぶことになります。型消去された C 側は引数で受け取ります。

`String` は `Array<u8>` 1 field なので同じ形になります。どちらも 1 word には
lower されません。

その代償として、**header を書き換える method は呼び出し側の header に届く
必要があります**。copy を渡して copy を育てても、呼び出し側の `data` は
解放済みの pointer のままです。そこで:

- std の mutator は `&var self`、reader は `&self` を取る
- `&var Array<T>` parameter は caller storage を渡す
- capture 束縛は `let` 束縛と同じく slot を得る
  (`if build() |parts|` のあとの `parts.append(..)`)
- runtime の `KizuString` は header そのもの。`read_dir` は header を値で返す

`Array` を union payload や struct field に置けるよう、layout table は
`Array<T>` を 32 byte / 8 align として答えます。

### 却下

| 案 | 却下理由 |
| --- | --- |
| pointer header のままにする | 空の array の allocation が消えない。これが変更の目的そのもの |
| 空のときだけ null handle にし、最初の append で header を確保する | 読み取り経路すべてに null 分岐が増え、`len()` が branch を持つ。header を値にすれば分岐ごと消える |
| `elem_size` を header に残す | 型消去された C 側が自分で読めるのは利点だが、それは呼び出し側が既に持っている値。array of array では全要素がその複製を運ぶ |
| header を値にしつつ mutator は値で受け、書き戻しを caller が行う | 呼び出しの形が method ごとに変わり、`xs.append(v)` が `xs = xs.append(v)` になる。所有権の規則ではなく綴りの規則が増える |
| `Array` を copy 禁止にして borrow だけで扱う | `Array` は既に move-only。問題は copy の可否ではなく、mutator が header のどこに書くか |

## Consequences

- 空の array は allocation を持たない。`Array<T>` を field に持つ struct は、
  その array を使わないなら何も確保しない
- header は 8 byte ではなく 32 byte なので、値で渡す場所は stack を 4 倍使う。
  compiler 自身の型解決の再帰がこれに当たり、最適化なしで build した
  compiler は 1 段あたり約 150KB 使う。generic instantiation の入れ子上限
  (SPEC §13)を 64 から 32 に下げたのはこのため。上限は「compiler が死ぬ前に
  診断を出す」ために在るので、短い方の stack が保てる値に置く
- reader が `&self` を取るので、読み取りは header の copy ではなく address を
  渡す。header の copy が読み取りごとに消える
- 残るコストは、`String` が `Array<u8>` 1 field なので 32 byte あることと、
  この compiler が Go の `[]string` の機械移植で `String` を 12,718 箇所に
  持っていること。`allocator` も header から落として 24 byte(Rust の `Vec`
  と同じ 3 word)にする案があり、それは `deinit(allocator)` と、container の
  要素にまで tie 検査を広げる規則とセットになる
- `kizu_array_new` は無くなった。header を確保するものが無い
