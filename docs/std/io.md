# std::io

`std::io` は標準入出力を明示的な `Io` capability 経由で扱います。書き込みと
読み込みは失敗し得るため、すべて error union を返します。

```kizu
pub fn blocking() -> Io

pub fn write_stdout(io: Io, bytes: &[]u8) -> std::io::Error!void
pub fn write_stdout_line(io: Io, bytes: &[]u8) -> std::io::Error!void
pub fn write_stderr(io: Io, bytes: &[]u8) -> std::io::Error!void
pub fn write_stderr_line(io: Io, bytes: &[]u8) -> std::io::Error!void

pub fn read_stdin(
    io: Io,
    allocator: Allocator,
    limit: std::mem::Limit,
) -> std::io::Error!std::string::String

pub fn read_stdin_into(
    io: Io,
    out: &var std::string::String,
) -> std::io::Error!void
```

`write_stdout` と `write_stderr` は bytes をそのまま書きます。`*_line` は bytes の
後に改行を追加し、途中の書き込みを含む失敗をそのまま返します。formatting や
暗黙の allocation は行いません。

`read_stdin` は EOF まで読んだ bytes を caller-owned `String` として返します。
allocator と上限は caller が明示します。`read_stdin_into` は既存の `String` に
追記し、独自の storage を持ちません。

`std::io::Error` は `WriteFailed`、`ReadFailed`、`OutOfMemory`、`IoFailing`、
`LimitExceeded` を持ちます。
