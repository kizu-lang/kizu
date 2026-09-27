# ADR-0097: 固定長配列 `[N]T` は inline に並ぶ copy 値にする

## 背景

`[N]T` は heap を使わない local 限定の buffer として入った。値は alloca の pointer
そのもので、frame より長く生きる経路を検査せずに済むよう、field・引数・返り値・
要素に置くことを禁じた。数値計算ではこの制限が形を崩す。座標 `[3]f64` や行列
`[3][3]f64` は field として至る所に現れ、struct に崩すと添字が全部 accessor になる。

## 決定

`[N]T` を、N 個の T を inline に並べた値にする。T が copy なら `[N]T` も copy で、
struct field・union payload・引数・返り値・container の要素に置ける。大きいものは
`&[N]T` / `&var [N]T` で借りて渡す。

- 表現は struct と同じ aggregate(LLVM `[N x T]`、wasm は frame 上の bytes)。
  view(`as_slice` / `as_bytes` など)は配列の storage(slot・field の番地・
  `&var` 引数)を指す。storage の無い値の読む view は、値を slot に写してから取る
- 要素は copy data(数値・bool・enum・copy aggregate・その入れ子の配列)に広げる。
  view と owner を要素にしないので、配列は常に copy で、index は等幅の cell を指す
- 生成は `[N]T{}` の zero 埋め(数値と bool だけ)か、`[N]T{a, b, ...}` で N 個を
  並べる。初期化子のない宣言は持たない
- 要素は添字で直接読み書きする(`a[i]`、`s.m[i] = x`)。範囲外は trap、literal の
  添字は compile 時に弾く。書き込みは配列の place への書き込みとして、field の
  書き込みと同じ借用規則に掛ける。view は長さを消して関数へ渡すときに使う
- 長さは literal か static な整数(static parameter、`comptime for` の capture、
  その括弧つき整数演算 `[(n * 2)]f64`)。instance ごとに評価し、1 未満は呼び出しの誤り

## なぜこの形か

値として持てば返り値もコピーになり、frame を越えて残る pointer が生まれない。
local 限定にした理由(alloca の pointer を値として回すこと)が表現ごと消えるので、
新しい安全規則は要らない。field の配列の view は、既存の field 借用の規則で扱える。

## 却下した案

| 案 | 理由 |
| --- | --- |
| 利用側で `[3]f64` を struct に、scratch を `Array` に崩す | 添字がすべて accessor になり、書き誤りと読みにくさが増える。heap も使う |
| 値を alloca の pointer のまま field に置く | frame より長く生きる経路をすべて検査する規則が要る |
| `var buf: [64]u8;`(初期化子なし宣言) | 初期化子のない宣言という新しい形と、他の型への波及を止める特例が要る |
| `mem::stack_bytes<64>()` factory | 戻り型が値依存になり型検査の新機構が要る。確保が関数呼びに見える |
| `[]u8` を `[]u32` に reinterpret する view | byte 順が target 依存になり、同じ source が target ごとに違う bytes を出す |
| owner を要素にする | index は等幅の cell が前提。cleanup の所在が view に乗る |
| 要素の読み書きを view 経由に限る | 行列の `m[i][j]` が書けない。view を束縛する行が添字の数だけ増える |
| `a[lo..hi]` の直接 slicing | view の slicing と同じものの 2 本目の経路になる |
| enum や struct の配列も `{}` で 0 埋めする | 0 がその型の値とは限らない(enum の tag、struct の不変条件) |
| 要素数が N に足りない literal の残りを 0 で埋める | 書き忘れが黙って 0 になる |
