# ADR-0086: error は名前であって message ではない

Status: 採用

Supersedes: ADR-0030 の error payload 部分

Superseded by: 決定 3 のみ ADR-0087。決定 1 と決定 2 は有効。

## 背景

ADR-0030 は error payload を「owned copy of `[]u8` message」に固定し、
`error(message)` は message bytes を copy して所有すると書いた。

実装は copy していない。error union の失敗値は message slice をそのまま埋める。

```llvm
%r = insertvalue { i8, %kizu.slice.u8 } %base, %kizu.slice.u8 %message, 1
```

runtime も同じで、`kizu_slice_from_cstr` は文字列を指すだけである。今これが
動いているのは、実際の message が全て string literal だからにすぎない。

その制約は実害を出していた。file 操作の失敗は「ファイルが無い」「ディレクトリ
だった」「権限が無い」のどれでも `read file failed` を返していた。理由を message
に入れるには文字列を組み立てる必要があり、組み立てた文字列は呼び出しより長生き
できないため、入れられなかった。

### 所有すれば解ける、が Kizu では解けない

Rust はこれを所有で解いている。`std::io::Error` は `ErrorKind` と errno を持ち、
custom payload は heap に確保して所有する。

```text
kind=NotFound   raw_os_error=Some(2)   display=No such file or directory (os error 2)
kind=InvalidData                       display=port must be a number
```

Kizu で同じことをすると、`error(message)` が内部で確保することになる。

- user は `error("msg")` としか書いていないのに allocator が動く。
  ADR-0041 が避けると決めた hidden global allocator behavior である
- 確保は失敗しうる。確保に失敗したときに返す error を作るのに、また確保が要る

Zig が error に payload を持たせないのは、確保が明示的な言語だからである。Kizu は
同じ制約を選んでいる。

### 診断の置き場所は既にある

`std::kizu::diagnostic` は `Diagnostic` を持っている。

```kizu
pub struct Diagnostic {
    pub primary: FileSpan,
    pub message: []u8,
    related: Array<RelatedSpan>,
}
```

一方 `std/src/kizu/parser.kizu` は 2,442 箇所で `error("...")` を返している。

```kizu
Unsafe => return error("unsafe fn is not supported; use @requires_unsafe() fn");,
LBrace => return error("unexpected block");,
```

これは診断を error に載せているということであり、載せた時点で位置が失われる。
ADR-0072 が要求する `<category>: <summary> at <line>:<column>` を、error 経由の
診断は満たせない。error と `Diagnostic` は同じ役目を二重に持っている。

## 決定 1: error 値は名前であり、payload を持たない

error set を宣言する。

```kizu
error FsError {
    NotFound,
    PermissionDenied,
    IsDirectory,
}
```

error 値は set の要素そのものである。

```kizu
fn read(path: []u8) -> FsError!i64 {
    return FsError.NotFound;
}
```

payload は持たない。error 値は「何が起きたか」だけを運ぶ。

`error(message)` は廃止する。

## 決定 2: 詳細は診断が持ち、error は運ばない

失敗の詳細 -- 位置、期待値、実際の値 -- は `Diagnostic` が持つ。error 値は
「失敗した」ことと種類だけを運ぶ。Zig 自身の parser も同じ形で、`error.ParseError`
を返し、何が起きたかは `Ast.errors` が持つ。

これは新しい概念の追加ではなく、既にある `Diagnostic` を本来の役目に戻す変更で
ある。

`std::testing::fail(message)` は message を error に入れられなくなる。test の
失敗は `std::testing` が診断として出し、error 値は失敗したことだけを運ぶ。

## 決定 3: `!T` の error set は関数本体から推論する

> **置換済み (ADR-0087)。** この推論は実装されなかった。checker が持つのは
> 「`!T` はあらゆる set の member を受け取る」という受理規則 1 本であり、
> set を求める場所はない。ADR-0087 はその挙動を仕様として採用した。

```kizu
fn read_config() -> !i64 {
    let text = try std::fs::read_file(io, "config");   // FsError
    return try parse(text);                            // ParseError
}
```

`!T` は「この関数が返しうる error の集合」を意味し、checker が本体から求める。
Zig の inferred error set と同じ規則である。明示したい場合は `FsError!T` と書く。

## 影響

- error payload の所有権という問題が消える。error は整数 1 個になる
- error の生成が確保を必要としなくなる
- runtime の 51 箇所が固定 message ではなく error 値を返す
- `union` による typed error(`ConfigError!T`)は error set に置き換わる
- checker に error set の推論が入る
- conformance で runtime message を検査している 25 case が error 名に変わる
- `std/src/kizu/parser.kizu` は旧 selfhost と一緒に削除された。2,442 箇所の
  `error("...")` を `Diagnostic` に移す段階は不要になった

## 段階

1. error set の宣言と `FsError!T` を通す(推論なし、明示のみ)
2. `!T` の推論を入れる
3. runtime の error を error set に載せ替える
4. `error(message)` と `[]u8` payload を削除する

推論が runtime の載せ替えより先に来る。`std::fs::read_file` が
`std::fs::Error![]u8` を返すようになった時点で、`fn main() -> !void` からの `try`
は error 型の完全一致を満たさなくなる。推論がそれを吸収する。順序を逆にすると、
その間だけ通す規則を足すことになり、それは消える条件のない分岐になる。

各段階が単独で green になるようにし、`error(message)` は 4 まで残す。
