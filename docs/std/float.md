# std::float

浮動小数点値と文字列の変換、および bit 表現の読み書きです。

```text
std::float::append(allocator: Allocator, out: &var String, value: f64) -> std::mem::Error!void
std::float::parse(allocator: Allocator, text: []u8) -> std::mem::Error!?f64
std::float::is_nan(value: f64) -> bool
std::float::is_infinite(value: f64) -> bool
std::float::bits(value: f64) -> u64
std::float::from_bits(bits: u64) -> f64
```

`append` は、読み戻すと同じ値になる**最短**の 10 進を書きます。整数値は `.0` で終わり
(`100.0`)、小数は桁をそのまま持ち(`0.1`、`1234.5`)、21 桁を超える大きさと
1e-6 未満は指数形(`1e21`、`1.5e-7`)です。NaN は `NaN`、無限大は `inf` と `-inf`、
負の零は `-0.0` と書きます。

`parse` は 10 進を**最も近い** f64 に読み(同距離なら偶数へ丸め)、数でない文字列には
null を返します。受ける形は `append` が書くものと、人が書く普通の形です: 省略できる
符号、整数部と小数部(どちらか一方は省略でき、`1.` と `.5` も読める)、省略できる
指数、`NaN` / `inf` / `-inf`。桁区切りの `_` は受けません。f64 で表せない大きさは無限大に、小ささは零になります。

どちらも桁の生成と比較は幅の制限のない整数で行い、float の丸めに頼りません。その分
変換ごとに数回の確保が要り、代わりにどの target でも同じ答えになります。compiler の
float literal もこの `parse` で値を決めます(Go seed は `strconv`)。両者の一致は
`cmd/kizu` の `TestFloatText` が数千の値で確かめています。

`bits` は IEEE 754 binary64 の表現をそのまま返し、`from_bits` はその逆です。
`cast` は値を変換しますが表現には触れないので、bit を見る処理はこの 2 つの上に
書きます。両方とも trusted primitive で、float の支援が backend に頼るのはこの 2 つだけです。

言語としての float(型、literal、演算、cast)は SPEC §6.9.3 と §7 にあります。
