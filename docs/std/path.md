# std::path

slash 区切り path を text として扱う module です。file system を見ないので `Io`
を取らず、結果は入力の bytes だけで決まります。存在を確かめる正規化は
[`std::fs::real_path`](fs.md) です。

```kizu
pub fn join(allocator: Allocator, left: []u8, right: []u8) -> std::mem::Error!std::string::String
pub fn clean(allocator: Allocator, path: []u8) -> std::mem::Error!std::string::String

pub fn basename(path: []u8) -> []u8
pub fn dirname(path: []u8) -> []u8
pub fn extension(path: []u8) -> []u8
```

`join` と `clean` は caller-owned の `String` を返すので allocator が要り、
`deinit` も caller が呼びます。どちらも `.` を畳み、`..` を 1 つ上の segment に
適用し、連続 slash を 1 つにします。先頭が slash なら絶対 path として保ちます。
畳んだ結果が空になる場合は `.` を返します —— path は空文字列にならないためです。

```kizu
path::join(allocator, "/a", "../b/")   // "/b"
path::clean(allocator, "a//./b/../c/") // "a/c"
path::clean(allocator, "../a/..")      // ".."
path::clean(allocator, "")             // "."
```

`basename` / `dirname` / `extension` は入力への view を返すので、確保も
allocator も要りません。返る view は引数の bytes を指すので、その bytes が
生きている間だけ有効です。

- `basename` は末尾の segment。末尾 slash は無視します(`"a/b/"` → `"b"`)
- `dirname` は末尾 segment を除いた残り。segment が 1 つなら `"."`
- `extension` は末尾 segment の最後の `.` 以降。`"archive.tar.gz"` → `".gz"`、
  `".profile"` → `".profile"`、`.` が無ければ空

edge case は `examples/std_path_edges.kizu` が宣言しています。
