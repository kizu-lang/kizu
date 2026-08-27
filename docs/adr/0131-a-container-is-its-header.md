# ADR-0131: container は header そのもので、header への pointer ではない

## Status

Accepted.

## Context

`std::array::Array<T>`、`std::map::Map<K, V>`、`std::arena::Arena<T>` は
allocator が配った header への pointer でした。つまり **空の container が 1 回
allocation を要求します**。要素を 1 つも持たない array も、header の分だけ
確保してから始まります。

compiler が自分自身を compile するときの実測です。

| 項目 | 数 |
| --- | --- |
| 作られる array | 8,260,540 |
| 一度でも要素を持った array | 5,389,659 |
| array header allocation の占める peak memory | 51MB |

差の 287 万本は、確保して、何も入れず、解放しただけの header です。
`Vec<T>` / `ArrayList` を持つ言語はここに allocation を持ちません。値が
header だからです。

pointer header にはもう 1 つ、数字に出ない傷があります。**構築が確保するのに、
失敗を言えません。** `map::new` / `arena::new` は header を確保しますが
`Map` / `Arena` を返すので、確保に失敗すると runtime が null handle を返し、
後で最初に触った操作が無関係な行で trap します。std の他の場所では確保失敗は
`std::mem::Error!T` で、扱いを呼び出し側が決めます。同じ問い(memory が無い)に
2 つの違う答えがあるのは原理 7 に反します。

## Decision

**container は header そのもの。** 値が header で、構築は allocator に何も
頼みません。最初の `append` / `insert` / `add` が storage を買います。

| 型 | header | word |
| --- | --- | --- |
| `Array<T>` | `{data, len, cap}` | 3 |
| `Arena<T>` | `{data, len, cap}` | 3 |
| `Map<K, V>` | `{entries, len, cap, index, index_cap}` | 5 |

`Arena<T>` は `Array<T>` と同じ header です。要素の所有の仕方が同じで、違いは
backend より上にあります —— Handle が borrow ではなく index であること、途中の
要素を取り除かないこと。だから runtime に arena の entry point は 1 つも無く、
`arena.add` は array の append そのもので、**append 直前の len がそのまま
handle** です。

**method を宣言するのは std で、compiler ではありません。** 4 つの container は
どれも `lib/kizu/std/src/*/*.kizu` に method を書き、body は
`std::internal::builtin::*` primitive 1 つへ forward します。receiver がどう届くか
(`&var self` は header を書く、`&self` は読む)を言うのはその宣言 1 行で、型検査
も lowerer も slot 解析もそこを読みます。

header は `elem_size` を持ちません。`sizeof(T)` は compile 時に決まる値で、
compiler はそれを必要とする全ての呼び出しで知っています。header が持つと、
全ての array と、array of array の全要素が、呼び出し側の持っている値を
複製して運ぶことになります。型消去された C 側は引数で受け取ります。
`allocator` も同じ理由で持ちません(ADR-0132)。

**構築は確保しないので、失敗を言う必要がありません。** `array::new` /
`map::new` / `arena::new` は `!T` を返さず、確保の失敗は `append` / `insert` /
`add` の `!` に畳まれます。zig の `HashMap.init` / `ArrayList.init` が返す答えと
同じで、「失敗を言える場所でしか確保しない」ことで綴りが 1 つになります。

`String` は `Array<u8>` 1 field なので同じ形になります。どれも 1 word には
lower されません。

その代償として、**header を書き換える method は呼び出し側の header に届く
必要があります**。copy を渡して copy を育てても、呼び出し側の `data` は
解放済みの pointer のままです。そこで:

- std の mutator は `&var self`、reader は `&self` を取る
- `&var` container parameter は caller storage を渡す
- capture 束縛は `let` 束縛と同じく slot を得る
  (`if build() |parts|` のあとの `parts.append(..)`)
- runtime の `KizuString` は header そのもの。`read_dir` は header を値で返す
- **owner の borrow は address で渡る。** `&T` が copy で渡ると、その copy は
  元が後から所有するものを見なくなります。`b.show(b.put(v))` は receiver の
  copy を取ってから argument が `b` を育てるので、`show` が古い header を読む
  —— container が pointer だった頃は copy も同じ heap を指していたので無害
  でしたが、header を値にした時点で傷になります。owner かどうかは
  `ast.DeinitOwners` が既に持つ 1 つの定義(owner を持つものが owner)に従い、
  copy data は今まで通り値で渡ります

`Array` / `Arena` / `Map` を union payload や struct field に置けるよう、
layout table はそれぞれの header の大きさで答えます。

### 却下

| 案 | 却下理由 |
| --- | --- |
| pointer header のままにする | 空の container の allocation が消えない。これが変更の目的そのもの |
| 空のときだけ null handle にし、最初の append で header を確保する | 読み取り経路すべてに null 分岐が増え、`len()` が branch を持つ。header を値にすれば分岐ごと消える |
| `elem_size` を header に残す | 型消去された C 側が自分で読めるのは利点だが、それは呼び出し側が既に持っている値。array of array では全要素がその複製を運ぶ |
| header を値にしつつ mutator は値で受け、書き戻しを caller が行う | 呼び出しの形が method ごとに変わり、`xs.append(v)` が `xs = xs.append(v)` になる。所有権の規則ではなく綴りの規則が増える |
| `Array` を copy 禁止にして borrow だけで扱う | `Array` は既に move-only。問題は copy の可否ではなく、mutator が header のどこに書くか |
| pointer header のまま `map::new` / `arena::new` を `!Map` / `!Arena` にする | 失敗の綴りは 1 つになるが、空の map を作るだけで 703 箇所に `try` が付く。header を値にすれば確保自体が無くなり、`try` も要らない |
| `&T` の copy は残し、`b.show(b.put(v))` を borrow 検査で拒否する | 規則を 1 つ増やして利用者に区別を課す(原理 6)。`&T` が copy にも address にもなること自体が経路 2 本(原理 9)なので、そちらを畳む |
| Arena の `add` / `at` / `at_mut` は compiler の中に置いたままにする | 「receiver がどう届くか」の答えが 2 本になる。他の 3 つは宣言の `&var self` が答え、Arena だけ ownership checker の access table が答えるので、両者がずれても誰も気づけない(原理 9)。宣言を書けば 4 つとも同じ 1 本を読む |

## Consequences

- 空の container は allocation を持たない。container を field に持つ struct は、
  それを使わないなら何も確保しない
- `array::new` / `map::new` / `arena::new` は失敗しない。確保の失敗は
  `append` / `insert` / `add` が `std::mem::Error!` で言う
- header は 8 byte ではなく 24〜40 byte なので、値で渡す場所は stack を
  数倍使う。compiler 自身の型解決の再帰がこれに当たり、最適化なしで build した
  compiler は 1 段あたり約 150KB 使う。generic instantiation の入れ子上限
  (SPEC §13)を 64 から 32 に下げたのはこのため。上限は「compiler が死ぬ前に
  診断を出す」ために在るので、短い方の stack が保てる値に置く
- reader が `&self` を取り、owner の borrow が address で渡るので、読み取りは
  header の copy ではなく address を渡す。struct の byval copy が `&self` の
  呼び出しごとに消え、selfhost compiler の binary は 15.5MB から 11.1MB に、
  自分自身の check の peak RSS は 489MB から 465MB になった
- `kizu_array_new` / `kizu_map_new` / `kizu_arena_new` は無くなった。header を
  確保するものが無い。arena は entry point も無く、array のものを使う
- Arena が std 宣言を持ったことで、それが無いことに対する特例が消えた ——
  受け渡しを決める `containerSelfPassing`、slot 解析の arena 分岐、
  ownership から ir へ渡す `ArenaMethodWritesHeader`、型検査の手書き署名、
  そして受け皿だった `atHeader`。`arena.at` は呼び出しごとに展開されず、
  `Array.at` と同じく element 型ごとの wrapper 1 つになる
