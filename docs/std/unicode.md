# std::unicode

UTF-8 の検証と変換、正規化、書記素クラスタ分割です。入力も出力も `[]u8` と
`std::string::String` のままで、`String` に文字列としての意味は足しません。
host の Unicode 実装を呼ばないので、どの target でも同じ入力から同じ結果を返します。

```text
std::unicode::is_valid_utf8(bytes: []u8) -> bool
std::unicode::decode(bytes: []u8, position: i64) -> ?std::unicode::Decoded
std::unicode::append_utf8(allocator: Allocator, out: &var std::string::String, scalar: u32) -> std::unicode::GrowError!void
std::unicode::normalize(allocator: Allocator, bytes: []u8, form: std::unicode::Form) -> std::unicode::GrowError!std::string::String
std::unicode::is_normalized(allocator: Allocator, bytes: []u8, form: std::unicode::Form) -> std::unicode::GrowError!bool
std::unicode::next_grapheme_break(bytes: []u8, position: i64) -> std::unicode::Error!i64
```

`Decoded` は `scalar: u32` と `width: i64` を持ちます。`Form` は `Nfc` と `Nfd` です。
`GrowError` は `Error` と `std::mem::Error` の合成です。

## Unicode の版

**Unicode 18.0.0** に固定しています。表は暗黙には更新しません。版を上げるときは
`just unicode-tables` の版を書き換えて表と適合試験のファイルを生成し直します。
正規化の結果と書記素クラスタの境界は版によって変わることがあります。

表は Unicode Character Database から生成しています。Unicode License V3 の全文は
`lib/kizu/std/src/unicode/LICENSE-UNICODE.txt` にあります。

## 不正な UTF-8

不正な入力は置換せず、拒否します。U+FFFD に置き換える API はありません。

| 関数 | 不正な入力のとき |
| --- | --- |
| `is_valid_utf8` | `false` |
| `decode` | `null`。`position` が範囲外のときも同じ |
| `normalize`、`is_normalized` | `Error::InvalidUtf8` |
| `next_grapheme_break` | 読んだ範囲に不正な byte があれば `Error::InvalidUtf8` |

overlong 形式、サロゲート、U+10FFFF より大きい値は不正です。`append_utf8` はサロゲートと
U+10FFFF より大きい値を `Error::InvalidScalar` で拒否し、`out` を変更しません。

## 正規化

`normalize` は UAX #15 の NFC または NFD を新しい `String` で返します。`is_normalized` は
ASCII だけの入力なら確保せずに答え、それ以外は一時的に正規化して比較するので allocator を
受け取ります。

## 書記素クラスタ

`next_grapheme_break` は UAX #29 の extended grapheme cluster の境界を返します。`position` を
クラスタの先頭として読むので、クラスタの途中を渡すとそこから始まるクラスタの終わりを返します。
`position` が `bytes` の長さと等しいときはその位置を返し、範囲外は `Error::InvalidPosition` です。

```kizu
var position = 0;
while position < std::mem::len(bytes) {
    position = try std::unicode::next_grapheme_break(bytes, position);
}
```

## wasm の出力サイズ

表は使う関数の分だけ出力に入ります。`wasm32-browser` の `--opt` で、`print` だけの
program(437 byte)との比較です。

| 使う機能 | 出力 | 増加 |
| --- | ---: | ---: |
| `is_valid_utf8` | 4,088 byte | 3,651 byte |
| `next_grapheme_break` | 15,502 byte | 15,065 byte |
| `normalize` | 48,349 byte | 47,912 byte |
| 上の 3 つすべて | 59,812 byte | 59,375 byte |

## まだ無いもの

- **NFKC / NFKD**: 互換分解の表が要るので、必要になってから足します
- **大文字小文字の変換、照合順序、単語と行の分割**
