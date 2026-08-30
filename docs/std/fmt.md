# std::fmt

診断文字列を組み立てるための最小 formatting API です。
format string、locale、generic display trait、reflection は持ちません。
caller が `std::string::String` を用意し、formatting API はその buffer に
bytes を append します。buffer を育てる allocator は呼び出しごとに名指します
(ADR-0132)。

```text
std::fmt::append_i64(allocator: Allocator, out: &var std::string::String, value: i64) -> std::mem::Error!void
std::fmt::append_bool(allocator: Allocator, out: &var std::string::String, value: bool) -> std::mem::Error!void
std::fmt::append_bytes_literal(
    allocator: Allocator,
    out: &var std::string::String,
    bytes: []u8,
) -> std::mem::Error!void
```

hidden global allocator は使いません。
allocation failure は渡された allocator から `std::mem::Error::OutOfMemory`
として伝播します(ADR-0128)。
output は conformance test 向けに deterministic ASCII とします。
`append_i64` は 10 進表記で、負数には `-` を付け、`+` と不要な leading zero は出しません。
`append_bool` は `true` または `false` を出します。
`append_bytes_literal` は diagnostic 用の quoted byte string を出します。
printable ASCII byte のうち `"` と `\` 以外はそのまま出します。
`"`、`\`、newline、carriage return、tab はそれぞれ `\"`、`\\`、`\n`、`\r`、`\t`
として escape します。
その他の byte は uppercase hex の `\xNN` として escape します。

出力を受ける `String` の view 規則と、`&var std::string::String` を
out parameter に取る形については SPEC §14 を参照してください。
