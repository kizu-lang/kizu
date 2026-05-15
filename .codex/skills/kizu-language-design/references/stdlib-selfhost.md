# v0.2 Stdlib And Self-Host Guidance

Kizu v0.2 stdlib は、Kizu self-host compiler を可能にするための基盤です。
stdlib API を抽象的な便利関数としてだけ設計せず、compiler frontend の必要性から検証します。

## 推奨順序

1. `std::mem`
2. `std::array::Array<T>`
3. `std::string::String` と `[]const u8` helper
4. self-host frontend skeleton
5. `std::map::Map<K, V>`
6. `std::fs` / `std::path`
7. `std::io` / `std::process`
8. `std::testing`

## `std::mem`

まずは allocation-free な byte / slice helper から始めます。

```text
std::mem::equal_bytes(a, b) -> bool
std::mem::starts_with(bytes, prefix) -> bool
std::mem::index_of(bytes, needle) -> ?usize
std::mem::slice(bytes, start, end) -> ![]const u8
std::mem::trim_ascii(bytes) -> []const u8
```

方針:

- `index_of` は not found が正常系なので `?usize` を返す。
- `slice` は invalid range が理由付き caller error なので `![]const u8` を返す。
- safe API は raw pointer を返さない。
- raw pointer 実装は trusted stdlib または `unsafe` 内に閉じる。
- hidden default allocator を追加しない。

## Allocator Boundary

owned container には allocator を明示的に渡します。

```text
let allocator = std::mem::page_allocator();
let tokens = std::array::Array<Token>(allocator);
```

方針:

- `std::mem::Allocator` は visible type として存在してよい。
- allocator factory は明示的に呼ぶ。
- owned collection は明示的な cleanup / `deinit` を要求する。
- allocator の詳細が safe Kizu に raw pointer safety hazard として漏れてはいけない。

v0.2 の `Array<T>` prototype:

```text
std::mem::page_allocator() -> Allocator
std::array::Array<T>(allocator) -> std::array::Array<T>
array.append(value) -> !void
array.len() -> i64
array.capacity() -> i64
array.get(index) -> !T
array.at(index) -> !&T
array.at_mut(index) -> !&mut T
array.set(index, value) -> !void
array.deinit() -> void
```

`get` は v0.2 では copy element 限定です。non-copy token / AST node は
`at` / `at_mut` の local borrow view で扱います。
element borrow 中は `append`、`set`、`deinit` を拒否します。
mutable element borrow 中は array 全体の read も拒否します。
Array element は v0.2 では raw pointer、arena、handle、nested array、concurrency
capability type を拒否します。この拒否は struct field と union payload の中まで
再帰的に適用します。

## `std::string::String`

v0.2 の `String` prototype:

```text
std::string::String(allocator: Allocator) -> std::string::String
string.append_bytes(bytes: []const u8) -> !void
string.append_byte(byte: u8) -> !void
string.len() -> i64
string.as_bytes() -> []const u8
string.clear() -> void
string.deinit() -> void
```

`String` は owned byte buffer であり、primitive `string` ではありません。
`as_bytes` は local view として扱い、view 中の append / clear / deinit を拒否します。
append / clear は `&mut String` から呼べますが、deinit は owned local receiver 限定です。
UTF-8 validation、C ABI 変換、raw pointer exposure は v0.2 では実装しません。

## Self-Host Skeleton Rule

各 v0.2 stdlib API について、self-host compiler がどう使うかを記録します。

- lexer source scanning
- token list construction
- parser node list construction
- diagnostic string construction
- symbol table lookup
- source file loading
- CLI args and exit code
- component test assertions

skeleton が未存在の API を必要とした場合は、孤立した TODO を skeleton に作らず、
関連する stdlib issue を更新します。
