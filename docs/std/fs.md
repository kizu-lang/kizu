# std::fs

file system 操作を明示的な `Io` capability 経由で扱う module です。どれも失敗
し得るので、すべて `std::fs::Error!T` を返します。

```kizu
pub struct DirEntry {
    pub name: std::string::String,
    pub path: std::string::String,
    pub is_dir: bool,
}
pub struct Metadata { pub size: i64, pub is_dir: bool }

pub fn read_file(
    io: Io,
    allocator: Allocator,
    path: &[]u8,
    limit: std::mem::Limit,
) -> std::fs::Error!std::string::String
pub fn read_file_into(io: Io, allocator: Allocator, path: &[]u8, out: &var std::string::String) -> std::fs::Error!void
pub fn write_file(io: Io, path: &[]u8, bytes: &[]u8) -> std::fs::Error!void
pub fn exists(io: Io, path: &[]u8) -> std::fs::Error!bool
pub fn metadata(io: Io, path: &[]u8) -> std::fs::Error!std::fs::Metadata
pub fn real_path(io: Io, allocator: Allocator, path: &[]u8) -> std::fs::Error!std::string::String
pub fn read_dir(io: Io, allocator: Allocator, path: &[]u8) -> std::fs::Error!std::array::Array<std::fs::DirEntry>
pub fn rename(io: Io, from: &[]u8, to: &[]u8) -> std::fs::Error!void
pub fn create_dir(io: Io, path: &[]u8) -> std::fs::Error!void
pub fn remove_dir(io: Io, path: &[]u8) -> std::fs::Error!void
pub fn remove_file(io: Io, path: &[]u8) -> std::fs::Error!void
```

`read_file` は caller-owned の `String` を返します。allocator と上限は caller が
明示し、上限超過は `OutOfMemory` ではなく `LimitExceeded` です —— memory は足りて
いて、入力の方を断ったからです。`read_file_into` は既存の `String` に追記し、
fs は自分の storage を持ちません。

`write_file` は file が無ければ作成し、あれば内容を置き換えます。host が write を
途中までしか受理しない場合は残りを続け、進捗が止まった場合は `WriteFailed` です。

`real_path` は symlink と `.` / `..` を file system に対して解決するので、path が
存在している必要があります。存在を要求しない純粋な正規化は
[`std::path::clean`](path.md) です。

`read_dir` は entry を **name 順**で並べた `Array<DirEntry>` として返します。
host の `readdir` が返す順は file system 次第で 2 台の間で食い違うので、順序を
ここで確定させています。返る Array は caller が所有するので `deinit` が要り、
各 `DirEntry` の `name` と `path` も owned `String` です。`read_dir` に渡したものと
同じ allocator で `entries.deinit(allocator)` すると、entry の文字列と Array storage を
まとめて解放します。読むときは `entries.at(index)` で entry を borrow し、
`entry.name.as_bytes()` / `entry.path.as_bytes()` で view を得ます。

`wasm32-wasi` では path は host が最初に preopen した directory からの相対 path
として解決します。compiler 自身は directory を公開しません。preopen が無い場合や
その外へ出ようとした場合は `PermissionDenied` です。conformance example は必要な
repository-relative directory を末尾の `dir:` metadata で宣言し、runner がその
capability だけを Wasmtime へ渡します。`real_path` の結果もその capability からの
相対 path であり、host の絶対 path は guest へ公開しません。

`wasm32-browser` は filesystem capability を提供しません。`std::fs` に到達する program は
JavaScript で filesystem を偽装せず、build 時に target 非対応として拒否します。

`Metadata` と `DirEntry` の layout は C runtime が `KizuFsMetadata` /
`KizuFsDirEntry` として写しています(`internal/native/build.go`)。field を変える
なら両方を変えます。

`std::fs::Error` は `InvalidPath`、`NotFound`、`PermissionDenied`、`IsDirectory`、
`NotDirectory`、`AlreadyExists`、`DirectoryNotEmpty`、`NoSpaceLeft`、
`TooManyOpenFiles`、`ReadFailed`、`WriteFailed`、`OutOfMemory`、
`OperationFailed`、`IoFailing`、`LimitExceeded` を持ちます。
