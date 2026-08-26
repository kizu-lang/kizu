# std::string

明示 allocator capability を受け取る owned byte buffer です。

```text
std::string::new(allocator: Allocator) -> std::string::String
std::string::from_bytes(allocator: Allocator, bytes: []u8) -> std::mem::Error!std::string::String
std::string::join(allocator: Allocator, parts: &std::array::Array<std::string::String>, separator: []u8) -> std::string::GrowError!std::string::String
std::string::trim_space_in_place(value: &var std::string::String) -> void
string.append_bytes(bytes: []u8) -> std::mem::Error!void
string.append_byte(byte: u8) -> std::mem::Error!void
string.append_string(other: &std::string::String) -> std::mem::Error!void
string.reserve(additional: i64) -> std::string::GrowError!void
string.truncate(length: i64) -> std::string::Error!void
string.len() -> i64
string.capacity() -> i64
string.as_bytes() -> []u8
string.as_mut_bytes() -> &var []u8
string.clear() -> void
string.deinit(allocator: Allocator) -> void
```

`string` primitive は追加しません。
`std::string::new()` のような hidden default allocator は使いません。
`from_bytes` は source の `[]u8` を copy した owned `String` を返します。確保に失敗した
途中のbufferは解放し、sourceはborrowしたままです。
`append_bytes` は source の `[]u8` を move せず、owned buffer に copy します。
`append_byte` は 1 byte を追加します。
`append_string` は borrow した別の `String` の bytes を copy します。source は
move されません。
`join` は `parts` と `separator` を borrow し、明示 allocator 上に独立した
`String` を作ります。separator は隣り合う要素の間だけに入ります。空の Array は
空の String を返します。要素が 1 つでも borrow を返り値へ escape させず、独立した
owner を返すため bytes を copy します。出力長が `i64` に収まらない場合は
`std::string::Error::InvalidLength`、確保できない場合は
`std::mem::Error::OutOfMemory` です(`GrowError` は両 set の合成、ADR-0128)。
`trim_space_in_place` は String の両端から次の Unicode White_Space 25 code point を
除きます: U+0009–U+000D、U+0020、U+0085、U+00A0、U+1680、U+2000–U+200A、
U+2028–U+2029、U+202F、U+205F、U+3000。この集合は Unicode 15.0 / Go 1.22 と
一致し、暗黙には更新しません。U+180E、U+FEFF、妥当な U+FFFD、不正 UTF-8 byte は
空白ではありません。不正 byte は 1 byte の非空白として保持します。処理は確保せず、
capacity を保ち、内部の byte と空白は変更しません。
`reserve` は少なくとも `additional` byte 分の追加 capacity を確保します。負の
`additional` は `Error::InvalidLength`、確保できない場合は
`std::mem::Error::OutOfMemory` です。
`truncate` は length を短くし、capacity は保持します。範囲外の length は
`Error::InvalidLength` です。
`capacity` は現在の capacity を `i64` で返します。
capacity の増加戦略は実装が決めます。保証するのは `capacity() >= len()` だけであり、
特定の値に依存するコードは実装を固定してしまうため書けません。
`clear` は length を 0 にしますが、capacity は保持します。
UTF-8 validation、C ABI string 変換、raw pointer exposure、
owned bytes 取り出し、String 専用 comparison、String 専用 indexing / slicing は実装しません。
`std::string::String` の public behavior は `lib/kizu/std/src/string/string.kizu` に実装します。
private `std::array::Array<u8>` storage の上に構成し、safe Kizu に
raw pointer は公開しません。mutable backing は `as_mut_bytes` の
exclusive borrow 経由でだけ公開します(ADR-0096)。public
`std::mem::OwnedBytes` または `std::bytes::Buffer` は、raw storage
provenance の仕様後に検討します。

`String` は non-copy / move-only です。view(`as_bytes` / `as_mut_bytes`)の
束縛規則、view が生きている間の禁止事項、`deinit` の receiver 制限は
checker が持つ規則なので SPEC §14.4 にあります。
`trim_space_in_place` は method ではなく通常の `&var String` 引数なので、排他 borrow、
field path、live view との競合には他の `&var` 引数と同じ規則がそのまま適用されます。
