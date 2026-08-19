# std::string

明示 allocator capability を受け取る owned byte buffer です。

```text
std::string::new(allocator: Allocator) -> std::string::String
string.append_bytes(bytes: []u8) -> !void
string.append_byte(byte: u8) -> !void
string.append_string(other: &std::string::String) -> !void
string.reserve(additional: i64) -> !void
string.truncate(length: i64) -> !void
string.len() -> i64
string.capacity() -> i64
string.as_bytes() -> []u8
string.as_mut_bytes() -> &var []u8
string.clear() -> void
string.deinit() -> void
```

`string` primitive は追加しません。
`std::string::new()` のような hidden default allocator は使いません。
`append_bytes` は source の `[]u8` を move せず、owned buffer に copy します。
`append_byte` は 1 byte を追加します。
`append_string` は borrow した別の `String` の bytes を copy します。source は
move されません。
`reserve` は少なくとも `additional` byte 分の追加 capacity を確保し、失敗時は `!void` を返します。
`truncate` は length を短くし、capacity は保持します。範囲外の length は `!void` error です。
`capacity` は現在の capacity を `i64` で返します。
capacity の増加戦略は実装が決めます。保証するのは `capacity() >= len()` だけであり、
特定の値に依存するコードは実装を固定してしまうため書けません。
`clear` は length を 0 にしますが、capacity は保持します。
UTF-8 validation、C ABI string 変換、raw pointer exposure、
owned bytes 取り出し、String 専用 comparison、String 専用 indexing / slicing は実装しません。
`std::string::String` の public behavior は `lib/kizu/std/src/string.kizu` に実装します。
private `std::array::Array<u8>` storage の上に構成し、safe Kizu に
raw pointer は公開しません。mutable backing は `as_mut_bytes` の
exclusive borrow 経由でだけ公開します(ADR-0096)。public
`std::mem::OwnedBytes` または `std::bytes::Buffer` は、raw storage
provenance の仕様後に検討します。

`String` は non-copy / move-only です。view(`as_bytes` / `as_mut_bytes`)の
束縛規則、view が生きている間の禁止事項、`deinit` の receiver 制限は
checker が持つ規則なので SPEC §14.4 にあります。
