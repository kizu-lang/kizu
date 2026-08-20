# std::json

JSON の encode と decode です。

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

## decode

```text
std::json::decode<T>(allocator: Allocator, bytes: []u8) -> !T
std::json::decode_ignore_unknown<T>(allocator: Allocator, bytes: []u8) -> !T
```

bytes から直接 `T` を組み立てます。document object を挟みません。DOM を持つと
それ自身の置き場を決める必要があり、untrusted 入力への防御を DOM が引き受け、
値への経路が「DOM を見る」と「型に流し込む」の 2 本になるためです(ADR-0115)。

key は宣言順で届く必要がありません。object を 1 回走査して各値の開始位置を
記録し、field ごとにその位置へ戻ります。

```kizu
let visit = try json::decode<Visit>(allocator, document);
```

`T` に来られる型は struct、`i64`、`bool`、`std::string::String`、
`std::array::Array<T>`、`std::json::Value` です。`[]u8` は decode できません —— 借用 view なので、
decode した bytes の持ち主がいなくなります。所有する `String` を使います。

### 型に無い key

`decode` は error にします。捨てると document が運んでいたデータが黙って
消えるためです。捨てることを選ぶ場合は名前でそう宣言します。

```kizu
try json::decode<User>(allocator, bytes);                 // 知らない key は error
try json::decode_ignore_unknown<User>(allocator, bytes);  // 捨てると名前で言う
```

### error

```text
std::json::Error::UnexpectedToken   その位置の bytes が JSON ではない
std::json::Error::MissingField      T の field を document が持たない
std::json::Error::UnknownField      document の key に対応する field が無い
std::json::Error::DuplicateField    同じ key が 2 回現れた
std::json::Error::InvalidNumber     JSON では有効だが i64 に入らない
std::json::Error::InvalidEscape     `\` の後が escape ではない、または孤立した surrogate
std::json::Error::DepthExceeded     入れ子が 128 段を超えた
```

原因と回復が違うものを分けています(原理 7)。壊れた入力、型と document の
食い違い、上限超過、確保失敗はそれぞれ別の error です。

### 制限

number は `i64` のみです。小数・指数は `InvalidNumber` にします。言語に float
演算が無いためで(SPEC §7 は `f64` を予約するだけ)、黙って切り捨てるより
error にします。

入力サイズの上限は持ちません。`[]u8` を渡すのは caller で、何 byte あるかは
既に caller が握っています。入れ子の深さだけ 128 段で止めます。

### Value

型を決めずに読む用途は `Value` が受けます。

```kizu
pub union Value {
    Null,
    Bool(bool),
    I64(i64),
    Str(std::string::String),
    Arr(std::array::Array<Value>),
    Obj(std::array::Array<Entry>),
}

pub struct Entry {
    pub key: std::string::String,
    pub value: Value,
}
```

普通の再帰 union です。document object 層ではありません —— `decode<Value>` は
他の型と同じ parser を通るので、untrusted 入力への防御は 1 箇所にあり、値への
経路も 1 本です。歩くのは普通の `match` です。

```kizu
var value = try json::decode<json::Value>(allocator, document);
defer value.deinit();
```

`Obj` が map でなく `Array<Entry>` なのは、map の value type が copy 限定で
`Value` が中身を所有するためです。配列は key を document の順で保ちます。

### 配列

`std::array::Array<T>` も `T` に来られます。各要素は要素型として decode するので、
入らない document は error です。要素自身が配列を持てるので、木全体が 1 呼び出しで
組み上がります。

```kizu
var numbers = try json::decode<array::Array<i64>>(allocator, "[1, 2, 3]");
defer numbers.deinit();
```

設計の経緯は [ADR-0112](../adr/0112-json-encoder-owns-output-and-traps-misuse.md)
(encoder が出力を所有し、誤用は trap)と
[ADR-0113](../adr/0113-comptime-structural-reflection-as-builtin-forms.md)
(`encode<T>` が乗る structural reflection)にあります。
