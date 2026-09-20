# ADR-0149: SIMD は固定名の 128-bit vector 型で明示する

Status: 採用

## 背景

struct `{f64, f64}` の配列を scalar で回す数値 kernel で、LLVM の loop
vectorizer が cost model を外し、要素の並べ替えと spill だらけの vector loop を
出して 20〜40% 遅くなった。scoped-noalias や runtime check 数の上限、interleave
抑制では直らず、loop vectorizer を止めたときだけ直る。一方で `[]f64` の単純な
loop は loop vectorizer で 1.6〜5 倍速いので、全体で止められない。

他言語は compiler が型の形で vectorize の判断を覆すことをしない。C / Rust /
Zig / Swift は LLVM に任せ、速さが要る所は利用者が明示 SIMD(`@Vector`、
`std::simd`)か SoA で書く。Kizu もそれに倣い、明示 SIMD を言語に持つ。

## 決定

1. vector 型は固定名の 128-bit 値 `f64x2 f32x4 i64x2 i32x4 i16x8 u64x2 u32x4
   u16x8`。wasm simd128、NEON、SSE2 のどれでも 1 register で、backend が分割
   や合成をしない。より広い型は additive に足せる。
2. 生成は `f64x2{a, b}`(lane 数ちょうど)、lane 読みは整数 literal の `v[0]`
   (範囲は compile 時)、演算は同じ型同士の `+ - *`、float の `/`、符号付きの
   単項 `-`。lane の書き込み、比較、shuffle、reduce は v1 に入れない。swap は
   `f64x2{v[1], v[0]}` で書け、LLVM が shuffle に畳む。
3. vector は copy data。struct field、`Array<T>` の要素、view の要素に置ける。
4. IR は `vector.new` / `vector.lane` の 2 命令。LLVM は `<N x T>` と
   `insertelement` / `extractelement`、wasm は `v128` と `splat` /
   `replace_lane` / `extract_lane`、算術は lane ごとの shape 命令。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| compiler が「aggregate 要素の loop は loop-vectorize しない」と判断する | LLVM の cost model を型の形で覆す規則は他言語に無く、LLVM の version で意味が変わる。struct の単純 loop で 0〜8% 損もする |
| loop ごとの pragma(`vectorize(disable)`) | 1 件の cost model の穴のために構文を足す。明示 SIMD があれば要らない |
| `Vector<N, T>` の任意 N | wasm では 128-bit 超を分割する必要があり、backend に合成 code が要る。固定名なら 1 register の事実だけを言える |
| SoA(re / im の `[]f64` 2 本)のみ | Kizu を変えずに済むが、Zig 等が AoS + `@Vector` で書く形と比較できない |
| 8-bit lane(`i8x16`) | wasm に `i8x16.mul` が無く、演算の表が型で欠ける。要るときに足す |
