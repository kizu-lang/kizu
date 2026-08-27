# ADR-0134: arena は自分の instance を見分ける

Status: 採用

Issue: なし(handle provenance の検査が閉じていなかったことの解消)

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

この形は理論上の話ではない。selfhost には同時に生きる `Arena<string::String>`
が 5 本あり、うち 2 本は同じ struct の隣り合う field だった。

## 何を見分ける必要があるか

一度は marker —— arena ごとに field を持たない struct を宣言させ、
`Arena<T, M>` / `Handle<T, M>` の 2 引数にする —— で閉じた。閉じたのは
**宣言単位**、つまり「別々の `arena::new` から作られた arena どうし」だけ
だった。実際に混同が起きるのはそこではない:

```kizu
// compiler/src/internal/typ/typ.kizu
fn equal_node(
    left: &TypeTree,  left_handle:  arena::Handle<Node>,
    right: &TypeTree, right_handle: arena::Handle<Node>,
) -> bool
```

`TypeTree` は instance ごとに自分の `nodes` arena を持つ。marker があっても
両方 `Handle<Node, TreeNodes>` なので、`left_handle` と `right_handle` を
入れ替えた呼び出しは型が通り、別の木の別の節点が黙って返る。`Table` も
`equal(left_table, right_table)` で 2 本同時に生きるので同じことが起きる。

**閉じるべき単位は宣言ではなく instance だった。** marker の前提「1 宣言 =
1 arena」は、marker のために書き換えを強いた当のコードで成立していない。

## 決定

**arena は、自分の handle が何から数え始めるかを header に持つ。**

```text
Arena<T> header = { data, len, cap, origin }
origin          = (instance << 32) + 1
handle          = origin + index
index           = handle - origin
```

1. **`Arena<T>` / `Handle<T>` は 1 引数に戻す。** marker 宣言も、marker を
   1 か所しか名乗れない全プログラム検査も、std だけが marker 抜きを書ける
   方言も、型検査より下で marker を落とす erasure も無くなる。

2. **instance は実行時に配る。** `arena::new` が `kizu_arena_origin()` を呼び、
   返った値を header の 4 word 目に置く。1 つの `arena::new` を 2 回実行すれば
   2 つの instance になる —— 宣言ではなく instance を見分けるとはこのこと。
   確保は増えない(ADR-0131 の「構築は何も確保しない」はそのまま)。

3. **読み取りは引き算 1 つだけを足す。** `at` は今も `sub handle, 1` で index
   を作って `icmp ult index, len` にかけていた。その `1` が `origin` になる。
   別 arena の handle は 2^32 の倍数だけずれた index になり、**既存の範囲検査が
   そのまま弾く**。命令は GEP + load 1 つ、branch は 0 本追加。失敗は
   `kizu_panic_arena_handle`(「invalid arena handle」)で、範囲外 handle が
   前から使っていた経路と同じ。

4. **handle は 8 byte のまま、niche もそのまま。** `origin` は最低でも
   2^32 + 1 なので、live な handle が 0 になることはない。`?Handle<T>` は
   ADR-0133 のまま 1 word。

5. **arena の長さは 2^32 未満に留める。** これが 2^32 のずれを「どの長さより
   大きい」にしている前提なので、`add` はここを越える成長を
   `kizu_panic_arena_full`(「arena is full」)で止める。検査は `add` に置く
   —— そこは既に allocator に確保を頼んでいる。

6. **instance は再利用しない。** 解放済み arena の origin を配り直すと、その
   arena の古い handle が次の arena の要素を名指す。これはこの仕組みが拒否
   したい読みそのもの。42.9 億本を使い切ったら wrap ではなく報告して止まる
   (`arena instances exhausted`)。Rust の `std::thread::ThreadId` が
   「単調 + 枯渇で panic」を採っているのと同じ形。

## 閉じるもの、残るもの

閉じる: **同じ型・同じ宣言・同じ関数から作られた 2 つの instance の取り違え。**
field を経由しても container に入れても関数境界を越えても、handle が自分の
出自を値として持つので消えない。上の `equal_node` の形が止まる。

同一 frame の local 2 本は ownership checker の `arenaID` が今も **compile 時に**
止める。binding 単位の静的検査と instance 単位の実行時検査は補完関係で、
どちらも消せない。

残る: **診断が型 error ではなく実行時 trap であること。** 宣言違いの取り違えは
marker なら compile 時に出せたが、通らない経路の取り違えは実行しないと出ない。
Kizu は closure を持たない(SPEC §7)ので、instance ごとに綴れない brand を
作る rank-2 の手(Rust の `GhostCell` / `qcell::LCell` / `generativity`)は
輸入できない。ここが型で閉じられる天井で、原理 5 の「型で閉じられる検査」に
当たらない。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| marker で宣言単位に名乗る(`Arena<T, M>`) | 閉じるのが宣言単位で、実際に混同が起きる instance 単位が閉じない。`typ.kizu` の `equal_node` はその形のまま残った。対価は arena 1 本につき空 struct 1 つ(selfhost で 37)、marker を 1 か所しか名乗れない全プログラム検査、std だけが marker 抜きを書ける方言、両実装 5 file ずつの erasure。1 つの性質に compile 時と実行時の機構を 2 本持つのも原理 9 に反する |
| handle を `{index, id}` の 2 word にする | 8 → 16 byte で ADR-0133 が削った分が戻る。`?Handle` の niche も失う |
| id を handle の上位 32bit に置き、`at` で `icmp eq` する | 動くが、mask / shift / load / icmp / and で 5 命令。`origin` を引くだけなら範囲検査が兼ねるので 1 命令で済む。実測でも selfhost の `check compiler` が +7% と +2% で分かれた |
| origin を最初の `add` で配る | `new` が `zeroinitializer` のままになるが、`add` 全部が「まだ配られていないか」を見ることになる。add は new より桁で多い |
| instance を配らず、`arena::new` の位置から compile 時に定数を振る | 定数は宣言単位なので、marker と同じ穴が同じ形で残る |
| 解放済み instance を再利用する(`qcell::QCellOwner` の形) | 古い handle が新しい arena の要素に一致しうる。`deinit` 後の handle 使用は SPEC §10 が禁じている読みで、そこを偶然通す形は入れない |
| 何もせず「安全だが値は不定」と文書化する | Rust の `slotmap` / `generational-arena` / Bevy の `Entity`、Zig compiler の `Zir.Inst.Index` が取っている線。値が嘘になることを認める以上、原理 1 に反する |
| `Arena.at` を `?&T` にする | 実際に起きる混同は範囲内で起きるので、optional は「不在」ではなく「取り違え」を表すことになる。読み取り全部に capture の 2 行を課しても意味が合わない |
| 検査を release build で外す | Kizu に検査を落とす build mode は 1 つも無く、ADR-0084 は検査を IR に置いて backend には実装だけさせると決めている。同 ADR の「停止する失敗」表には「無効な arena handle」が既に載っている |

## 帰結

- **`arena.at` は GEP + load 1 つ増える。** branch は増えず、`origin` は `len`
  と同じ header の同じ cache line にある。selfhost の `check compiler` は
  0.86s → 0.88s(+2.3%)、peak RSS は変化なし。
- **arena header は 24 → 32 byte。** 先頭 3 word は array のままなので、
  `kizu_array_append` / `kizu_array_deinit` は pointer を渡されて先頭だけを
  読み、arena は今も runtime entry point を持たない(ADR-0131)。割れるのは
  「Arena は Array と同じ header」という layout 上の記述だけ。
- **`arena::new` は定数ではなくなった。** `zeroinitializer` の代わりに
  `kizu_arena_origin()` の呼び出し 1 つを書く。確保はしないので ADR-0131 の
  「構築は失敗しない」は変わらない。
- **arena は 2^32 - 1 要素で満杯になる。** 越える `add` は停止して報告する。
- ADR-0105 の「handle の存在は静的検査が保証する」という前提は成立しない。
  `Arena.at` が `?` を付けない根拠は、範囲検査と同じく **trap する** ことに
  なる。
