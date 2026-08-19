# ADR-0114: errdefer は receiver が move された時点で退役する

Status: 採用

Issue: #1634(decode の設計検証で見つかった前提。#1626)

## 背景

`errdefer` は fallible な owner 構築のために入れた(ADR-0075)。今の checker は
live な errdefer entry の receiver を **各 `try` の地点で** 検査し、move 済みなら
拒否する。

```text
move error: errdefer cleanup receiver `child` was moved before an error path
```

このため、次の形が書けない。

```kizu
fn build(allocator: Allocator, count: i64) -> !array::Array<string::String> {
    var parent = array::new<string::String>(allocator);
    errdefer parent.deinit_all();

    var i = 0;
    while i < count {
        var child = string::new(allocator);
        errdefer child.deinit();
        try child.append_byte(cast<u8>(97));
        try parent.append(child);   // ここで child は parent の所有になる
        i = i + 1;                  // 次の反復の try で errdefer が invalid になる
    }
    return parent;
}
```

「子を作る → 親に move する → まだ失敗しうる操作が続く」は、木を組む
すべての builder の中核の形である。JSON decode の array / object 構築も、
symbol table の構築も、path の分解もこの形になる。

今書ける回避形は 1 つだけで、それは傷を隠す。

```kizu
try parent.append(try make_child(allocator));
```

名前を付けないので errdefer が要らなくなるが、`append` が失敗したときに
`make_child` が返した owner は誰にも解放されない。しかも local binding が
ないので、leak 検査もこれを見ない。**書ける形が leak する形で、書きたい形が
拒否される。**

SPEC §6.3.1 は既に move を義務の移転として扱っている。

> 成功 path で owner を move / return することは `errdefer` の実行を要求しません。

不足しているのは、この考えを error path へ伸ばすことだけである。

## 決定

**errdefer entry は receiver が move された時点で退役する。** 退役後の error
exit path では実行しない。

1. **退役は静的に決まる。** cleanup は各 exit 地点に emit される
   (`internal/ir/defer.go` の `errorCleanups` / `emitCleanups`)。move 地点で
   entry を退役させれば、それ以降の exit には cleanup が並ばない。runtime flag は
   要らない。
2. **move state は既存の追跡をそのまま使う。** 分岐の片方でだけ move された
   値は、合流点で move 済みとして扱われる(既存の挙動)。errdefer の退役も
   同じ判定に従う。
3. **explicit `deinit` と borrow は従来どおり error のままにする。**
   move は義務の**移転**で、新しい owner が続きを持つ。`errdefer x.deinit()`
   の隣に `x.deinit()` を書くのは義務の**二重履行**で、ほぼ確実に書き手の
   誤りである。診断を残す方が原理 1 に合う。
4. **退役した receiver への再代入で errdefer は復活しない。** 新しい owner は
   新しい義務を持つので、必要なら errdefer を書き直す。書き直さなければ
   既存の leak 検査(`would leak on this try's error exit`)が捕まえる。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| runtime flag で errdefer の実行可否を持つ | cleanup は静的に emit されているので不要。実行有無が source の move で決まる方が読める(原理 2) |
| move の直後に errdefer を明示解除する綴りを足す | 解除語が move の数だけ増える(原理 10)。move は source に見えており、そこから導出できる(ADR-0098 の構造導出) |
| inline 形(`try parent.append(try make_child(...))`)を推奨形にする | callee の失敗で子が漏れ、しかも検査もされない。傷を隠す(原理 1) |
| move 後の try を error のままにし、builder は毎回 helper 関数に切り出させる | 切り出しても親へ move した瞬間に同じ壁が来る。関数境界は問題を移すだけで解かない |

## 影響

- SPEC §6.3.1 の errdefer receiver 規則を書き換える。「その errdefer が実行され
  得る各 error exit path」の集合から、receiver を move した以降の path が外れる
- `internal/ownership`: move 時に `liveErrDefers` から該当 entry を退役させる。
  `validateErrDeferReceivers` の move 分岐は消える
- `internal/ir`: `deferFrames` の entry を move 地点で退役させる。emit 側の
  変更は不要
- 既存の negative example が 1 つ減り、builder の positive example が入る

## 再評価条件

- 条件付き consume(分岐の片方でだけ `deinit`)が leak しないよう検査を
  強めるとき(#1631)、errdefer の退役条件も同じ move state を見ているので
  合わせて見直す
- errdefer が `defer` と別の cleanup stack を持つ設計に変えるとき
