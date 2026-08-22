# std::fmt

診断文字列を組み立てるための最小 formatting API です。
format string、locale、runtime reflection は持ちません。heterogeneous な entry
point は compiler builtin ではなく、`parts: ...` を使う通常の Kizu function です。

```text
std::fmt::append(
    out: &var std::string::String,
    parts: ...,
) -> !void
std::fmt::format(
    allocator: Allocator,
    parts: ...,
) -> !std::string::String
std::fmt::append_i64(out: &var std::string::String, value: i64) -> !void
std::fmt::append_bool(out: &var std::string::String, value: bool) -> !void
std::fmt::append_bytes_literal(
    out: &var std::string::String,
    bytes: []u8,
) -> !void
```

`append` / `format` は `[]u8`、`std::string::String`、`i64`、`bool` を標準で
扱います。それ以外の型では、次の static contract に対応する通常の method を呼びます。

```kizu
pub contract Display {
    fn append_display(out: &var std::string::String) -> !void;
}
```

```kizu
struct User {
    name: []u8
}

fn (self: &User) append_display(out: &var std::string::String) -> !void {
    return std::fmt::append(out, "User(", self.name, ")");
}

impl std::fmt::Display for User;
```

`impl` は §16 の structural contract assertion なので任意です。method が無い型は
call の concrete instance で compile error になります。runtime interface や vtable は
作りません。`std::fmt` は `part` を共有 borrow して method を呼ぶため、canonical method
は `self: &T` で宣言し、値を consume / mutate しません。`Display` は書式指定ではなく、
canonical な人間向け表現を 1 つだけ表します。別の表現が必要な場合は、名前の付いた
helper または別の formatter type を使います。

part は書いた順に出力されます。`value`、`&value`、`&var value` は capture 後も通常の
値・共有 borrow・可変 borrow のままです。non-copy owner は通常 `&owner` で渡します。
`move owner` は ownership を `std::fmt` に移しますが、canonical formatting は共有 borrow
しかしないため、未消費 owner として compile error になります。

`append` は caller が用意した `String` に追記します。`format` は明示された
allocator から新しい owned `String` を作って返します。

```kizu
let message = try std::fmt::format(
    allocator,
    "type error: `", name,
    "` expects ", expected,
    ", got ", actual,
);
```

hidden global allocator は使いません。format string parser、erased argument array、
runtime type tag / dispatch もありません。capture は compile time に固定 parameter と
直列の append に展開されます。`format` 自体の runtime cost は `String` の確保・伸長と
各 append だけです。
allocation failure は `String` の allocator から `!void` error として伝播します。
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
