# ADR-0125: `?Owner` を struct field に置く

Status: 採用

Issue: #1632

## 背景

`?std::string::String` は struct field に置けなかった。

```
error: type error: struct field `Visitor.nick` cannot store an optional owner yet;
       only plain copy data and arena handles can be optional fields
```

理由は「条件付き deinit 義務を field の中に隠す」だった。ところが**同じ意味を
union で綴ると通っていた**。

```kizu
pub union Slot {
    Kept(string::String),
    Vacant,
}
```

`Slot` は `?String` そのもので、条件付き義務も同じだけある。違うのは owner
payload union が explicit `deinit` を要求されること(ADR-0075)だけだった。
原理 7 の裏は「意味が同じなら畳む」で、同じ意味の 2 つの綴りの片方だけが
通っている状態だった。

## 制限が実際に止めていたもの

制限を外すと **double free する**。理由は元の doc comment が言うより強い。

```kizu
fn show(visitor: &Visitor) -> void {   // 借用で受けている
    if visitor.nick |held| {
        held.deinit();                  // 通ってしまう
    }
}
```

`?Owner` の capture は payload を**所有として**束縛する。`Array.pop()` が返す
`?T` ではそれが正しい —— pop は中身を渡すからである。しかし field 読みが
返す `?T` の payload は field storage の中にあり、開いても渡されない。

同じ問題を container accessor は既に解いていて、`Map.at` は `?&V` という
borrow optional を返す(ADR-0104)。field 読みだけがその区別を持っていなかった。

## 決定

**`?Owner` を struct field に置けるようにし、開いた payload の所有は読む場所で
決める。**

| 読む場所 | capture が束縛するもの |
| --- | --- |
| その型自身の `deinit(self: T)` | owner。consume できる |
| それ以外(借用越し、他の関数、値で受けた local) | borrow。consume は拒否 |

例外が `deinit` だけなのは、そこが receiver を値で受けて分解している場所
だからで、`self.field.deinit()` に既に与えている direct field cleanup の例外と
同じ位置である。`match` が owner union payload に対して持つ分け方とも同じで、
新しい機構は増やしていない(原理 6、原理 9)。

判定の既定は **borrow** である。所有になるのは、payload を positively 渡す形
—— `Array.pop()` のような call —— のときだけで、それ以外は読みとして閉じる。
既定が逆だと、知らない形が 1 つ増えるたびに double free が戻ってくる
(原理 8: 迷ったら閉じたまま出す)。field path の深さもこれで効かなくなる:
`outer.inner.nick` も storage の読みで、そこからの cleanup は ADR-0067 が
既に拒否している。

義務は既存の検査に載せる。`?Owner` field は普通の owner field と同じに数える。

- `deinit` が field を開かない → `deinit of X must consume owner field Y`
- `deinit` が開いて payload を捨てる → `owned value held is never deinitialized`。
  開くことがその field の義務を果たす唯一の経路なので、捨てると誰も解放しない
- live な field への代入 → `owner field x.f is overwritten before cleanup`。
  `Array.set` が owner element に対して出すのと同じ拒否である

`while` の条件で owner field を開いて consume することは拒否する。条件は毎回
同じ storage を読むので、1 周目が解放した payload を 2 周目が解放する。開いて
consume するのは `if` である。

view の optional(`?[]u8`)は引き続き field に置けない。view の義務は借用で、
それを開く capture は field 型を読む規則から見えないままである。

`?Owner` は union payload、static argument、borrow の対象、関数の parameter に
は引き続きできない(ADR-0101)。変えたのは struct field 1 か所だけである。

## 影響

`std::json` は `?std::string::String` field を decode / encode できるように
なった。JSON で最も普通の「省略できる文字列 field」がこれで書ける。

`std::meta::field<T, f>(value)` は field を借用で読む形なので(ADR-0113)、
その capture も borrow を束縛する。`encode_fields` はこの形で optional field を
開いている。

#1633(値で受けた aggregate から owner field を取り出せない)はこの ADR では
解いていない。`?Owner` field も他の owner field と同じくその制限を受ける。
