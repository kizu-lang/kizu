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

同じことが `std::arena::Handle<T>` にも言えます。handle は arena 内の index で、
`add` が返した index は必ず要素を名指します。index 0 も live なので、そのままでは
空き値がありません —— **handle を index からずらして持てば 0 が空きます**。
handle は opaque な ID で等値比較しかされない(SPEC §10)ので、ずれは綴りから
見えません。ずらす量は arena が自分の instance を名乗る `origin` で、最下位の
1 がここで言う bias にあたります(ADR-0134)。

## Decision

**要素が不在を綴れるなら、optional はその綴りを使う。**

```text
?std::mem::Box<T>          ->  ptr    (null が不在)
?&T / ?&var T              ->  ptr    (null が不在)
?std::arena::Handle<T>     ->  i64    (0 が不在。handle は index + arena の origin)
?T (T が niche を持つ field を持つ struct) -> T
その他の ?T                 ->  { i8, T }  (従来どおり)
```

不在の bit 列は常に zero です。そのおかげで niche は**深さを持てます**: struct が
field 越しに niche に届くなら、その struct の `zeroinitializer` がそのまま不在に
なります。だから niche は「それを持つ field への path」でしかありません。

**struct は、niche を持つ最初の field の niche を継ぎます。** これが効く形は
selfhost compiler の AST です:

```text
Handle<T>                                  i64、bias で 0 が空く
Expression { node: Handle<ExpressionNode> }  単一 field なので niche を継ぐ
?Expression                                {i8,{i64}} = 16  ->  i64 = 8
IndexExpr { target, index?, start?, end? }  112  ->  88
ExpressionNode (union、最大 variant + tag)  120  ->  96
```

optional は niche の供給源にしません。niche optional は zero を不在に使い切って
いるし、tagged optional の空き値は tag byte のものです。

`opt.null` は要素の zero、`opt.some(x)` は `x`、`opt.has(o)` は niche がある word
を読んで zero と比べたもの、`opt.value(o)` は `o` です。命令はむしろ減ります ——
tag の組み立てが消え、存在判定は tag を作るのに使っていた比較そのものになります。

`Array.at` / `Map.at` / `Arena.at_mut` が返す borrow optional は、runtime が
返す null 可能 pointer をそのまま渡すだけになります。

### niche を持たない型

`Array<T>` / `Map<K, V>` / `Arena<T>` は header そのもの(ADR-0131)で、空の
container の data pointer は null です。null が live な値なので niche はありません。

### 前方参照

niche optional の `opt.some` / `opt.value` は命令を書きません —— payload が
optional そのものだからです。loop header の phi は body が後で定義する値を
名指すので、その名前を「自分の register」と決めうちにすると宙に浮きます。
emitter は関数を書き始める前に「どの結果が何の別名か」を記録し、phi の
operand をそこから解きます。

## 却下

| 案 | 却下理由 |
| --- | --- |
| tag を残す | 再帰的な節点が倍のまま。`?Box<T>` が 8 byte で足りるのに 16 byte 払う |
| pointer の null だけを niche にする(この ADR の初版) | handle と、handle を包む struct が対象外のままになる。selfhost の AST は `?Handle` を包んだ struct の集まりで、そこが一番効く |
| すべての pointer を niche 扱いにする | container header の data pointer は空の container で null。null が live な値なので、不在と区別できなくなる |
| handle をずらさず、`i64::MIN` を空き値にする | zero が不在という 1 つの規則が崩れ、`zeroinitializer` が不在を意味しなくなる。struct が field 越しに継ぐ形もそこで止まる |
| tagged optional の tag byte の空き値(2..255)を niche にする | 不在を表す bit 列が zero でなくなる型が生まれ、深さを持つ niche の前提が壊れる |
| enum / error set の未使用 tag を niche にする | 同じ理由。加えて、どの tag が空いているかは宣言ごとに違い、型ごとに追う表を要求する |
| niche を言語に見せる(`?T` の size を綴れるようにする) | 表現は言語の定義ではない。SPEC は `?T` が何を意味するかだけを持つ |

## Consequences

- `?Box<T>` と `?&T` は 1 word。`Node { left: ?Box<Node>, right: ?Box<Node> }` は
  16 byte になり、binary-trees の RSS は 46MB から 23MB —— C と同値、Rust の
  24MB より小さい。CPU も 1.48s から 1.25s
- selfhost compiler では 100 の型が縮む。`ast::ExpressionNode` 120 -> 96、
  `ast::DeclarationNode` 152 -> 136、`ownership::Binding` 360 -> 328。
  tag を持つ optional の型定義は 195 から 104 に減り、binary は
  11.79MB から 11.29MB になる
- **peak RSS は `check compiler` で 441MB から 450MB に増える。** 静的な型の
  大きさはすべて減っているので、増えたぶんは malloc の size class と
  fragmentation で、handle の niche を切ると main と同じに戻る。小さい入力では
  逆に減る(`check examples/arena_tree.kizu` で 19.9MB -> 19.6MB)
- 表現は言語から見えない。`?T` の意味も規則も変わらず、SPEC は触っていない
- `zeroinitializer` が引き続き不在を意味する。null pointer も 0 の handle も
  zero bit 列で、tag 0 と同じ
- niche を持つ型が増えたら、`hasNiche` の base case に足す。struct が継ぐ規則は
  そこから自動的に効く
