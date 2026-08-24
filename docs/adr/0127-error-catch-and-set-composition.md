# ADR-0127: `catch` による error 処理と set の合成宣言

Status: 採用

## 背景

error の消費手段は `try`(伝播)しか無く、call site で処理する構文が無かった。
SPEC は「宣言した set は `match` で網羅的に分岐できる」と約束していたが、
error 値を束縛する構文が無いため到達不能だった。

buildcache 移植(#1675)で具体例が 2 つ出た。壊れた metadata entry を miss
として扱う(decode の失敗だけ回復する)と、並列 eviction で消えた file を
skip する(NotFound だけ回復し、残りは伝播する)。どちらも「特定の失敗だけ
回復する」形で、`try` では書けず、Go 側の bug fix を selfhost に移植できない
まま gap として記録していた。

また set をまたぐ変換が存在しないため、複数 module の error が合流する関数は
`!T` としか書けず、その caller は失敗の種類に触れなかった。

## 決定

SPEC §11.1 / §11.2 の通り。要点は 3 つ。

1. **`catch` は `E!T` だけを処理する。** expression 形
   `f() catch default`(capture 無し、`orelse` と同段・同 guard 形)と、
   statement 形 `if f() |v| { } else |err| { }`(`err: E`、enum と同じ
   `match` で分岐)。error union 条件の `if` は `else |err|` 必須。
   catch された error は関数を出ないので `errdefer` は実行しない。
2. **`!T` は伝播専用のまま。** catch できるのは set を宣言した union だけ
   なので、handler は常に compile 時に網羅検査できる(原理 5)。
3. **set は和として宣言する。** `error CacheError = FsError or JsonError;`
   は member 集合の和で、新しい値を作らない(set を 1 つだけ書けば別名)。
   値を宣言するのは `{ }` 形だけで、`=` 形の右辺は宣言済み set の参照だけ。
   自前 member も足す合成は、その member を持つ set を宣言して和に入れる。
   合成された member は元の set の値そのものなので「set をまたぐ変換は
   存在しない」規則はそのまま残り、`try` の伝播検査は member 集合の
   部分集合検査に一般化する。宣言の形(`Name = A or B`)は、将来 contract
   の合成に実需が出た場合も `contract C = A and B;` と同じ形に一般化できる。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `orelse` を error union にも使う | 正常な不在(`?T`)と回復する失敗(`E!T`)は意味が違う(原理 7)。同じ word に相乗りさせない |
| `!T` も catch できるようにする(束縛値は任意 set の member) | 束縛値の型として「任意 set の member」型(Zig の `anyerror` 相当)の新設が要る(原理 6)。set が無いので `match` の網羅検査も効かない。`E!T` 限定なら handler は常に網羅検査でき、後から `!T` へ開くのは additive(原理 8) |
| expression 形の `catch \|err\| expr` | optional の「capture は statement 形だけ」と非対称になる。実需が出たら additive に足せる(原理 8) |
| error union 条件の `if` で `else` 省略を許す | 失敗を黙って捨てる経路になる(原理 1)。捨てるなら捨てると書く |
| 合成を型位置に直接書く(Zig の `A \|\| B`) | 名前の無い set が signature ごとに再綴りされ、grep できず drift する(原理 3)。「`E` は宣言済みの error set」の規則も破る。Kizu には和を束縛する型 alias も無い |
| 和を `or` でなく `\|\|` と綴る | Kizu に `\|\|` token は無く、boolean も `and` / `or` の語で綴る。`\|x\|` は capture の綴りで、pipe の連続は紛らわしい |
| 宣言 body に `include <set>,` を書いて合成する | error 宣言でしか使えない新 keyword が要り、値の宣言と和の宣言が 1 つの body に混ざる。`=` 形なら右辺は set 参照だけなので、新 member との曖昧さが宣言の形で消え、keyword が不要 |
| 合成した member を合成側の名前で再 export する(`CacheError::NotFound`) | 複数の合成元が同名 member を持つときの衝突規則が要る。出自の名前のままなら新規則が要らず、grep で出自に辿り着ける(原理 3) |
| member 一覧に set 名を裸で書いて合成する(marker 無しの body 形) | 同一 file 内の set は裸の identifier で参照できるため、新 member の宣言と区別できない |
| 汎用の型 alias + 型演算で合成する | alias 単体では合成できず、型式の文法と評価も要る。error に限れば `=` 形 1 つで足りる。汎用 alias は別の問いで、実需が出たら独立に検討する |
