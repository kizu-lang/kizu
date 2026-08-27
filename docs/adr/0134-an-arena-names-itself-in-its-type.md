# ADR-0134: arena は自分を型で名乗る(marker)

Status: 採用

Issue: なし(handle provenance の静的検査が閉じていなかったことの解消)

## 背景

`Handle<T>` は要素の index で、どの arena のものかを値としても型としても
持っていなかった。どの arena 由来かは ownership checker が binding ごとに
追う「既知 provenance」だけが見ていて、その規則は自分でこう認めていた:

> An unknown side — an arena that arrived as a borrow, a handle read out of a
> field — passes.

つまり handle が field や container を経由すると出自が消える。実行できる形で
示すと:

```kizu
struct Ref { who: arena::Handle<User>, }

let alice = try a.add(allocator, User { name: "alice" });
let _     = try b.add(allocator, User { name: "frank" });

let saved = Ref { who: alice };
print(b.at(saved.who).name);   // frank
```

範囲内に落ちるので trap もせず、**別 arena の別要素が黙って返る**。メモリ安全
ではあるが値としては嘘で、原理 1(傷を隠さない)に反する。

この形は理論上の話ではなかった。selfhost には同時に生きる
`Arena<string::String>` が 5 本あり、うち 2 本は同じ struct の隣り合う field
だった。

## 決定

**arena の型は、何を保持するかに加えて、どの arena かを名乗る。**

```kizu
pub struct AstTexts {}                                    // marker

pub struct Ast {
    pub texts: arena::Arena<string::String, AstTexts>,
}
pub struct Text {
    pub node: arena::Handle<string::String, AstTexts>,
}
```

1. **`Arena<T, M>` / `Handle<T, M>` の 2 引数にする。** `M` は marker で、
   field を持たない struct でなければならない。`add` / `at` / `at_mut` は
   `M` の一致も要求する。要素型が違う arena は今日すでに型で分かれていたので、
   閉じるのは「同じ `T` の arena が複数」という残りの穴だけになる。

2. **marker は実行時に何も持たない。** handle は i64 のまま、arena header は
   24 byte のまま、`?Handle<T, M>` の niche(ADR-0133)もそのまま。命令は
   1 つも増えない。marker は型検査より下へ渡る前に消える。

3. **marker は method の型パラメータではなく receiver のもの。** arena の
   method はどの marker でも同じように動くので、std は marker 抜きで宣言する:

   ```kizu
   fn (self: &std::arena::Arena<T>) at<T>(handle: std::arena::Handle<T>) -> &T
   ```

   型検査が receiver から marker を読み、std が書いた `Handle<T>` に付け直す。
   結果として型パラメータは 1 つのままで、instance が marker ごとに複製される
   ことがない。marker 抜きの綴りを書けるのは std source だけとする。

4. **1 つの marker を名乗れる `arena::new` は program 全体で 1 か所。** 2 回
   目は型 error にする。marker が 2 つの arena に付いたら、その 2 つの handle
   が交換可能に戻り、marker の存在意義が消えるため。

5. **erasure は 1 か所で行う。** `typ::EraseArenaMarker` が型検査より下の
   境界で marker を落とし、`typ::AttachArenaMarker` が std signature に
   receiver の marker を戻す。ownership / IR / LLVM / wasm は marker を
   知らない。

## 閉じるもの、残るもの

閉じる:

```kizu
var a = arena::new<User, Left>(allocator);
var b = arena::new<User, Right>(allocator);
let alice = try a.add(allocator, User { name: "alice" });
let saved = Ref { who: alice };
b.at(saved.who);
// error: `Arena.at` expects Handle<User, Right>, got Handle<User, Left>
```

field 経由でも container 経由でも関数境界越しでも閉じる。型が運ぶので、
ownership checker の「既知 provenance」規則の届かない場所がなくなる。

残る:

- **同じ 1 か所の `arena::new` から作った 2 つの arena。** `ast::new()` を
  2 回呼べば 2 つの `Ast` があり、その `texts` handle は交換可能なまま。
  これを閉じるには rank-2(呼び出しごとに新しい綴れない brand)が要り、
  それには closure が要る。Kizu は closure を持たない(SPEC §7)ので輸入
  できない。ここが天井。
- ただし **同一関数内の local 2 本**は ownership checker の `arenaID` が
  今も止める。marker(宣言単位)と `arenaID`(binding 単位)は補完関係で、
  どちらも消せない。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `Arena.at` を `?&T` にする | 実際に起きる混同は範囲内で起きるので optional は発火しない。安全を買えないのに、読み取り全部に capture の 2 行を課す |
| `Handle` に実行時 `arena_id` を持たせる | handle が 8→16 byte(ADR-0133 で削った分が戻る)か、header が 24→32 byte で ADR-0131 の Array 共有が割れる。しかも id は有限で、完全には閉じない |
| 何もせず「安全だが値は不定」と文書化する | Rust の `slotmap` / `generational-arena` が取っている線。原理 5(型で閉じられる検査は compile 時に閉じる)に反する |
| marker を宣言位置から compiler が自動生成する | 綴れないので struct field や signature に書けない。handle は field に入るので使えない |
| marker を method の型パラメータ `at<T, M>` にする | 使われない型パラメータが instance を分け、`Arena.at.User.Left` と `Arena.at.User.Right` が同じ body で 2 つ出る。receiver から読めば問題自体が消える |
| marker の一意性を検査しない | 2 つの arena が同じ marker を名乗れてしまい、決定 1 が閉じたはずの穴が戻る |

## 帰結

- 混同は診断が型の不一致になった。以前は「呼び出し側が署名から契約を再導出
  して検査する」機構が要ったが(`arenaHandlePairs`)、いまは単に型が合わない。
- **arena 1 本につき marker 宣言が 1 行増える。** 情報を持つ 1 行(どの arena
  かを名乗る)なので原理 10 の「定型」ではないと判断したが、コストではある。
- **1 つの arena 型に constructor は 1 か所。** 決定 4 の帰結で、selfhost の
  `typ` は `Builder.nodes` と `Parser.nodes` を別々の `arena::new<Node>` で
  買っていたため、1 つの `new_node_storage` に畳む必要があった。
- **handle を持つ struct は marker を具体的に名指す。** 関数は
  `fn read<M>(a: &Arena<Item, M>, h: Handle<Item, M>)` と marker を型
  パラメータにできるが、struct にはできない(SPEC §7: full generics を
  実装しない)。実コードでは handle を持つ struct は特定の arena のものなので
  問題にならないが、「どの arena の handle でも入る struct」は書けない。
- ADR-0105 の「handle の存在は静的 provenance 検査が保証する」という前提は
  この ADR 以前は成立していなかった。いまは成立する。
