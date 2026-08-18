# ADR-0108: copy 判定を struct / union の構造から導出する

Status: 採用

Issue: #1597

## 背景

copy 型は scalar の固定列挙だけで、宣言された struct / union は中身に
関係なく copy にならなかった。`struct Point { x: i64, y: i64 }` の複製は
i64 2 つの複製と同じで cleanup 義務を生まないのに、`Array.get` /
`Map` value / optional 保存の copy 制限に一律で弾かれ、コレクションに
触れた瞬間からアクセスが `at` の borrow に限られて、値で受けたい署名まで
`&T` が伝播していた。SPEC §8 の旧記述も矛盾していた: copy 型列挙は
scalar のみなのに、非 copy 列挙は「non-copy field を**含む** struct」と
書かれ、全 field copy の struct について 2 つの列挙が反対の結論を出す。

## 決定

copy 判定を型の構造から導出する。**scalar・enum・error set・copy
aggregate だけを field / payload に持つ struct / union は copy aggregate
であり、copy 型である。**注釈は導入しない(Rust の `#[derive(Copy)]` も、
opt-in の `impl Copy` も採らない)。導出できる情報を人間に書かせる構文は
儀式である(ADR-0098 と同じ判定)。

境界は 3 つで、いずれも既存 regime を保存する:

* **明示 `deinit` を宣言した型は move-only に留まる。** 宣言された
  cleanup contract は consumption 義務であり、義務を負う値の複製は
  義務の二重化になる。Array.deinit が explicit-deinit 型を owner element
  として扱う既存判定とも一致する。
* **view(`[]u8`)を含む struct は copy にしない。** ADR-0100 の
  borrow-class / tie 規則に従う。capability(`Io` / `Allocator`)や
  raw pointer を含む aggregate も同様に伝播させない。閉→開は additive
  なので、実需が出たら個別に開く(原理 8)。
* **再帰 path 上の revisit は非 copy。** 再帰型は indirection(`Box<T>`
  など非 copy)を要するため、実際には常にこの分岐より先に落ちる。

一般化した generic 宣言(`Pair<T>`)は field 型が具体化されるまで
判定できず、当面は非 copy(保守側)。

## 帰結

* `Array.get` / `get_or_panic`、`Map` の value、optional の copy 制限が
  そのまま copy aggregate に開く。API の規則文は変わらず、該当する型が
  増えるだけ。
* borrowed match の payload 束縛は「scalar は copy」から「copy 型は copy」
  に一般化される(§6.8)。
* 複製は純粋な bit copy で、hidden call / hidden allocation / 追加の
  cleanup 義務を含まない。move も同じ bytes の memcpy なので、copy 化で
  高くなる操作はない。大きい型を `&T` で受ける選択は署名に残る。
* 判定は types / ownership の両 checker に同型の `isPlainDataType` として
  実装した。lowering は `pop`(非 copy 対応済み)と同じ element load で、
  変更なし。

## 却下した代案

* **SPEC の記述だけ直す**: 誤読は消えるが `&T` 伝播のコストが残る。
* **opt-in の copy 表明**(`impl Copy for T;`): 導出可能な情報の書き写し。
  ADR-0098 が `borrows` 節を削除した判断と逆行する。
* **サイズ / field 数上限つきの伝播**: 型によって規則が変わり
  「書き方は 1 つ」から外れる。
