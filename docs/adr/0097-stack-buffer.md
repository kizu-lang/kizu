# ADR-0097: 固定長配列 `[N]T` は inline に並ぶ copy 値にする

## 背景

`[N]T` は heap を使わない buffer として、local 限定で入った。値は alloca の
pointer そのもので、frame より長く生きる経路を全部検査せずに済むよう、struct
field・引数・返り値・container の要素に置くことを禁じた。

数値計算のコードを書くと、この制限が形を崩す。3 次元の座標 `[3]f64` や 3×3 の
行列 `[3][3]f64` は struct の field として至る所に現れ、struct に崩すと添字の
読み書きが全部 accessor になる。

## 決定

`[N]T` を、N 個の T を inline に並べた値にする。T が copy なら `[N]T` も copy で、
struct field・union payload・引数・返り値・container の要素に置ける。大きいものは
`&[N]T` / `&var [N]T` で借りて渡す。

- 表現は struct と同じ aggregate(LLVM `[N x T]`、wasm は frame 上の bytes)。
  view(`as_slice` / `as_bytes` など)は配列の storage(slot・field の番地・
  `&var` 引数)を指す。storage の無い値の読む view は、値を slot に写してから取る
- 生成は `[N]T{}` の zero 埋め。初期化子のない宣言は持たない
- 要素は view で読み書きする

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
