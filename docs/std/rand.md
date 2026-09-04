# std::rand

seed から決まる擬似乱数列です。同じ seed は同じ列を、どの target でも出します。
test が入力を乱数から作るとき、失敗を seed 1 つで再現できるのはこの性質です。

```text
std::rand::new(seed: i64) -> Rng
Rng.next(&var self) -> i64            0 ..= 4294967086
Rng.below(&var self, bound: i64) -> i64   0 ..< bound、bound <= 0 は trap
```

```kizu
var rng = rand::new(42);
let dice = 1 + rng.below(6);
```

生成器は L'Ecuyer の MRG32k3a です(2 本の multiple-recursive generator の
合成、周期はおよそ 2^191)。shift と xor の生成器ではなくこれを選んだのは、Kizu に
bit 演算子が無く、overflow する乗算の意味も定めていないからです。MRG32k3a は
`*` `-` `%` だけで、値は 2^53 を超えません。未定義の算術に寄りかかる箇所はありません。

`new` はどの `i64` も seed として受け、状態を 16 手空回ししてから返します。近い seed が
近い値から始まらないためです。`next` は暗号用途の乱数ではありません。

seed を勝手に選ぶ関数はありません。毎回違う列が欲しい program は
`rand::new(process::unix_millis())` と書き、そう書いたことが source に残ります。

`below` の偏りは `bound / 2^32` 未満で、test の入力生成には見えない大きさです。
