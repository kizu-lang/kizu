# ADR-0006: comptime は展開だけを持ち、macro と interpreter は持たない

Status: 採用

## 背景

Kizu は Zig に近い低レベル指向を目指し、`comptime` は有力な機能である。一方、
macro / proc macro / AST rewrite は仕様とビルドを複雑にし、Kizu の明快さと仕様を
絞る方針を損なう。任意の Kizu 関数をコンパイル時に実行する interpreter も、
checker と IR の意味論をもう 1 つ持つことになり、build 時間を Go 並みに保つ方針と
衝突する。

## 決定

`comptime` は「型検査済みの Kizu コードを、コンパイル時に分かる値で**展開する**」
ものに限る。現在の形は SPEC §13 が持つ: `comptime <expr>`、`comptime if`、
`comptime for`(struct field / variant / 整数 range)、`comptime match`、
static 引数 `<n: i64>`。

- `comptime` expression が評価するのは整数・真偽値・文字列・型値・f64 と、その単項 /
  二項演算、`std::meta` / `std::target` 述語、f64 の `std::math` sin / cos / sqrt /
  pi / tau だけ
- 展開された各反復・各 branch は、その束縛のもとで型・ownership・borrow 検査する
- runtime の値は `comptime` expression から参照できず、runtime borrow が comptime
  境界を越えて escape することも禁止する
- filesystem access や build script 的副作用は持たない

整数 range の `comptime for` は runtime の `for` と同じ綴りに `comptime` を
置いた形で、capture は body では i64 の値、`comptime` expression では展開の整数。
展開数は 1024 で打ち切る: 展開は body の複製なので、それ以上は runtime の loop か
表が正しい形である。

f64 の comptime 値は、build 中の target で同じ式が実行時に計算する bit に畳む。
sin / cos は std の実装そのもの(Go seed は `internal/stdmath` が同じ算法を持ち、
Kizu compiler は `math::sin_with<fused>` を呼ぶ)で評価し、host の libm は使わない。
libm は OS と言語で値が違い(測った 80000 点で macOS libm と Go は std と 3276 /
20413 点食い違う)、`comptime` を付けると値が変わる言語になるからである。

## 却下した案

| 案 | 却下理由 |
|---|---|
| macro / AST rewrite API | 検査前の書き換えは型・所有権の検査対象にできず、読む側に展開後の形を要求する |
| 任意の Kizu 関数の comptime 実行(Zig の comptime interpreter) | checker / IR と別の評価器を持ち、両実装(Go seed と Kizu compiler)の一致点が増える。build 時間の上限も失う |
| `inline for` を別 keyword にする | `comptime for` が field / variant で既に同じ意味を持つ。展開するかどうかは `comptime` の 1 語で読める |
| 展開数の上限を持たない | body の複製が無制限に生成され、build 時間と binary size が source から読めなくなる |
| top-level `const` に comptime 評価した表を置く | global data を持つかは別の問い(hidden global runtime を持たない方針、§15)。必要になれば別 ADR |
| comptime の sin / cos を host の libm(Go の `math.Cos` など)で評価する | 実行時の std の値と 1 ULP ずれ、Go seed と Kizu compiler の出力も割れる |
| float literal の比較に ULP の許容幅を入れて両実装を突き合わせる | comptime と runtime の値のずれが残る。揃える方が安い |
| comptime float を持たず、定数は手で literal を書く | 正しく丸めた literal を人が計算し、test で照合する手間が残る |
