# std::float

浮動小数点値の bit 表現を読み書きします。

```text
std::float::bits(value: f64) -> u64
std::float::from_bits(bits: u64) -> f64
```

`bits` は IEEE 754 binary64 の表現をそのまま返し、`from_bits` はその逆です。
`cast` は値を変換しますが表現には触れないので、float を文字列にする、文字列を
float にする、といった処理はこの 2 つの上に書きます。両方とも trusted primitive で、
float の支援が backend に頼るのはこの 2 つだけです。

言語としての float(型、literal、演算、cast)は SPEC §6.9.3 と §7 にあります。
