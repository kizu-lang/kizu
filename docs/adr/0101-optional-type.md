# ADR-0101: optional 型 `?T` の最小セット

Status: 採用

Issue: なし(iterator protocol 検討の帰結)

## 背景

kizu には「値が無いかもしれない」を言う型が無く、不在を表すのに error
union を相乗りさせていた(`mem::byte_at` の OutOfBounds など)。原理 7
(意味が違うなら違うものにする)に照らすと、回復する失敗と正常な不在は
別物である。iterator の終端も不在であり、`?T` が無い間は has_next/next
の 2 メソッド規約しか書けなかった。

union で表現は可能だが、generic union が無いため API ごとに 2-variant
union を宣言することになり、原理 10(定型の量産)に反する。`?` prefix は
`?ptr<T>` として既に型文法にあり、`|name|` capture は `for 0..3 |i|` が
既に使っている。どちらの綴りも新規ではない。

## 決定

1. **`?T` 型**。element は scalar(明示幅整数 / bool / f32 / f64)と
   enum に限定する。`?ptr<T>` は raw pointer の nullable 綴りのままで
   対象外。**error union との合成 `E!?T` は許可する**: 意味論は一意に
   決まり(`try` は error 層のみ剥がす、`return` は成功値を二重に暗黙
   wrap)、fallible iterator が単一 signature で書ける。`??T` / `?!T`、
   struct field、union payload、static argument、borrow 対象は閉じた
   まま(optional を包めるのは error union だけ)。
2. **`null` literal**。文脈が `?T` の位置(return / 引数)でだけ書ける。
3. **暗黙 wrap**。`?T` 文脈の `T` は暗黙に wrap する。`!T` の成功 wrap
   と同じ既存規則の適用。
4. **消費は 3 形だけ**: `if opt |v| {} else {}`、`while opt |v| {}`、
   `opt orelse default`。presence を確かめない取り出しは存在しない。
   capture の綴りと意味は for-loop の `|i|` と同一。
5. **表現**は error union と同じ tagged payload(`{ i8, T }`)。IR は
   opt.some / opt.null / opt.has / opt.value の 4 命令。

## 却下した代替案

- **union のみで済ませる**: 追加ゼロだが、API ごとの union 宣言(原理
  10 違反)と `while true + match + break` の loop(match arm に break が
  無くそもそも書けない)が残る。
- **`x.?` 強制 unwrap**: checked index の trap と同類にできるが、v1 は
  「presence を確かめる 2 形 + 既定値」で閉じて出す(原理 8)。
- **match との統合**(`Some(v)` / `None` arm): variant 名の発明になり、
  union の match と別規則の match が並ぶ。capture 形 1 つで足りる。
- **owner element(`?String` など)**: payload の move-out 規則(ADR-0090
  相当)を optional に持ち込む必要があり、v1 の範囲外。

## 再評価条件

- owner / view element(`?[]u8`、`?String`)が必要になったとき(union
  payload の所有規則を optional に適用してから)。
- struct field への保存、static argument(`Array<?u8>`)、borrow
  (`&?T`)が必要になったとき。
- `x.?` 強制 unwrap、`orelse break/return` が必要になったとき。
- std の不在系 API(`mem::byte_at` など)を `?T` に移行するとき
  (breaking change として別 PR)。
