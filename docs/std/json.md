# std::json

JSON の encode です。decode はまだ持ちません(#1626)。

encode は明示 streaming encoder です。derive、hidden hook、method discovery は
持ちません。caller が begin / end と field 書き込みをすべて綴ります。

```text
std::json::encoder(allocator: Allocator) -> std::json::Encoder
std::json::encoder_with_spaces(allocator: Allocator, width: i64) -> std::json::Encoder
std::json::encoder_with_tabs(allocator: Allocator, width: i64) -> std::json::Encoder
encoder.begin_object() -> !void
encoder.end_object() -> !void
encoder.begin_array() -> !void
encoder.end_array() -> !void
encoder.begin_object_field(name: []u8) -> !void
encoder.begin_array_field(name: []u8) -> !void
encoder.write_i64(value: i64) -> !void
encoder.write_bool(value: bool) -> !void
encoder.write_null() -> !void
encoder.write_bytes(value: []u8) -> !void
encoder.write_i64_field(name: []u8, value: i64) -> !void
encoder.write_bool_field(name: []u8, value: bool) -> !void
encoder.write_null_field(name: []u8) -> !void
encoder.write_bytes_field(name: []u8, value: []u8) -> !void
encoder.finish_into(out: &var std::string::String) -> !void
encoder.deinit() -> void
```

`Encoder` は出力 buffer を所有する owner です。`std::json` は error set を
宣言しません。`!void` は所有 buffer の allocation 失敗だけを伝播します。

`write_*_field` は key と value を 1 呼び出しで書きます。key だけを書く
low-level API は持たないので、value のない key を残せません。

API の誤用は error ではなく **trap** です(ADR-0112)。次は回復可能な失敗
ではなく bug として、`runtime error:` を出して停止します。

* object の外で field を書く
* object を `end_array` で、array を `end_object` で閉じる
* 開いた container が無いのに `end_object` / `end_array` を呼ぶ
* object の中に field 名なしで値を書く
* top-level 値を 2 つ書く
* container が開いたまま、あるいは値を 1 つも書かずに `finish_into` を呼ぶ
* 62 段より深く入れ子にする

`finish_into` は完成した document を caller の `String` に append します。
`Encoder` は自分の buffer を持ち続け、`deinit` で解放します。

`encoder_with_spaces` と `encoder_with_tabs` は同じ document を行に分けて
書きます。`width` は 1 段あたりの個数で、`0` は compact 形と同じです。負の
width は trap です。要素の無い container は 1 行のままにします。整形は要素の
間の空白だけを変え、key の順序も値も変えません。

空白と tab を 1 つの関数の `[]u8` 引数にまとめません。`Encoder` が `[]u8`
field を持つと view を運べる型になり、view を貸せなくなります(§9)。
`write_bytes_field(name, string.as_bytes())` は文字列データを入れる主経路
なので、これは失えません。option record も持ちません。knob が 3 つ目に
なったときに、record が要るかを問い直します。

byte 列は決定的に escape します。`"`、`\`、newline、carriage return、tab は
`\"`、`\\`、`\n`、`\r`、`\t`、その他の control byte は lowercase hex の
`\u00XX` です。encode は UTF-8 validation をしません。

`encode<T>` は、値の形を §13.1 の comptime structural reflection で読み、
JSON document を書きます。

```text
std::json::encode<T>(
    allocator: Allocator,
    value: &T,
    out: &var std::string::String,
) -> !void
std::json::encode_value<T>(encoder: &var std::json::Encoder, value: &T) -> !void
std::json::encode_with_spaces<T>(
    allocator: Allocator,
    width: i64,
    value: &T,
    out: &var std::string::String,
) -> !void
std::json::encode_with_tabs<T>(
    allocator: Allocator,
    width: i64,
    value: &T,
    out: &var std::string::String,
) -> !void
```

encode できる型は次で閉じています。

| 型 | JSON |
| --- | --- |
| `i64` | number |
| `bool` | `true` / `false` |
| `[]u8` | string |
| `std::string::String` | string。所有する bytes を書きます |
| struct | public field の object。順序は source の宣言順 |
| `std::array::Array<T>` | array。順序は index 順 |
| `std::map::Map<[]u8, V>` | object。順序は `key_at` の挿入順 |
| `std::mem::Box<T>` | 中身そのもの。唯一所有は木なので形を足しません |
| `?T`(struct field) | 値があれば中身、無ければ `null` |

これ以外の型は **compile error** です(`std::meta::unsupported`)。黙って
何も書かないと、encoder 自身のテストでは捕まらない壊れた document が出る
ためです。public field を 1 つも持たない struct も同じ理由で拒否します。
状態が全部 private な型を `{}` と書くと、値が黙って消えるためです。
`std::arena::Handle<T>` は共有参照で、木である JSON と対応しないので
encode しません。union と enum はまだ持ちません。

`decode<T>` と `std::json::Value` はまだ持ちません。

設計の経緯は [ADR-0112](../adr/0112-json-encoder-owns-output-and-traps-misuse.md)
(encoder が出力を所有し、誤用は trap)と
[ADR-0113](../adr/0113-comptime-structural-reflection-as-builtin-forms.md)
(`encode<T>` が乗る structural reflection)にあります。
