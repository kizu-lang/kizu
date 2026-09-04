# std::fmt

診断文字列を組み立てるための最小 formatting API です。
format string、locale、generic display trait、reflection は持ちません。
caller が `std::string::String` を用意し、formatting API はその buffer に
bytes を append します。buffer を育てる allocator は呼び出しごとに名指します
(ADR-0132)。

```text
std::fmt::append_i64(allocator: Allocator, out: &var std::string::String, value: i64) -> std::mem::Error!void
std::fmt::append_u64(allocator: Allocator, out: &var std::string::String, value: u64) -> std::mem::Error!void
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
`append_u64` も同じ 10 進表記で、符号は出しません。
`append_bool` は `true` または `false` を出します。
`append_bytes_literal` は diagnostic 用の quoted byte string を出します。
printable ASCII byte のうち `"` と `\` 以外はそのまま出します。
`"`、`\`、newline、carriage return、tab はそれぞれ `\"`、`\\`、`\n`、`\r`、`\t`
として escape します。
その他の byte は uppercase hex の `\xNN` として escape します。

出力を受ける `String` の view 規則と、`&var std::string::String` を
out parameter に取る形については SPEC §14 を参照してください。

## print

```text
std::fmt::print<T>(value: T) -> void
```

builtin の `print(value)` はこの関数の呼び出しで、`T` は引数の型です
(SPEC §14.1)。改行付きで stdout に書き、`Io` capability を取らず、失敗を
報告しません —— 診断用に SPEC が認める唯一の例外で、プログラムの出力は
`std::io` を使います。

受け取る型と綴りは次のとおりで、これ以外の型は呼び出し位置で compile error
です(`std::meta::unsupported`)。

| 型 | 綴り |
| --- | --- |
| `[]u8` | そのまま |
| 整数(幅と符号を問わない) | `append_i64` / `append_u64` の 10 進 |
| `bool` | `true` / `false` |
| `f64`、`f32` | `std::float::append` の最短往復表現(`f32` は `f64` に広げて綴る) |
| enum | `Color::Green` |
| error set | 宣言元 set で修飾した member(`FsError::NotFound`) |

綴りは stack 上の固定 buffer(`std::mem::fixed_buffer`)で組み立てるので、
allocator を取りません。buffer は各種類の最長の綴りに足りる大きさで、
足りなかった場合は書き込み失敗と同じく何も出しません。
