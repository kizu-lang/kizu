# ADR-0155: module 直下の const は compile 時の値に付けた名前

## 背景

整数に名前を付けて固定長配列の長さに使う手段が無く、同じ意味の数と、そこから
計算した数が literal のまま並んでいた。static parameter は 1 つの関数の中でしか
読めず、struct の field にも書けない。

## 決定

`const name = expr;` を module 直下に置ける(SPEC §6.2.1)。

* 名前を読むことは、そこに右辺を書いたのと同じ。global な変数ではないので、
  storage も初期化順も持たず、hidden global runtime にならない。
* 置ける値は型でなく右辺の形で決める: literal、他の const、演算子、`cast`、
  struct / 固定長配列の literal、enum の値、`comptime` 式。owner や runtime の値は
  関数呼び出しを要するので、規則から外れる。
* 型は `let` に同じ式を書いたときの型。整数の const だけが `i64` として長さと
  static 引数に入る。
* module の外へ出さない。使わない const と、局所の名前による shadow は error。

## 却下した案

| 案 | 却下理由 |
|---|---|
| 置ける型を整数と `[]u8` に限る | 型ごとの許可リストは compiler を形で特殊化する(原理 11)。右辺の規則から自然に決まる |
| 型注釈 `const n: u32 = 48;` | `cast` は切り詰めるので注釈を `cast` に落とせず、検査付きの文脈が新しく要る。`cast<u32>(48)` を右辺に書けば足り、後から足せる(原理 8) |
| 型を持たない定数(使う場所の文脈で幅が決まる) | 「型を持たない値」という区別が増える(原理 6)。`let x = 1;` が `i64` である規則とずれる |
| `comptime let` という綴り | ADR-0004 が `const` をこの用途に残していた。宣言の位置で comptime と分かる |
| 長さに関数呼び出しを書く | 任意の関数の comptime 実行になる(ADR-0006) |
| `pub const` | 需要が出てから開く(原理 8)。閉→開は additive |
| 局所の名前が const を shadow してよい | 置き換えの意味なので、shadow を見落とすと別の値が黙って入る。import の shadow を禁じる規則とそろえる |
| static storage に置く global 定数 | hidden global runtime を持たない方針に反する。値の置き換えで足りる |
