# std::process

起動したプロセス自身について尋ねる module です。引数・環境変数・時刻・
実行ファイルの位置は kernel がこのプロセスについて答えるものなので、`Io`
capability を取りません。

```kizu
pub union ExitStatus { Success, Failure, Specific(u8) }

pub fn arg_count() -> i64
pub fn arg(index: i64) -> std::process::Error![]u8
pub fn env(name: []u8) -> ?[]u8
pub fn executable_path(allocator: Allocator) -> std::process::Error!std::string::String
pub fn monotonic_millis() -> i64
pub fn unix_millis() -> i64
pub fn exit_code(code: i64) -> i64

pub fn spawn_wait8(
    argc: i64,
    arg0: &[]u8, arg1: &[]u8, arg2: &[]u8, arg3: &[]u8,
    arg4: &[]u8, arg5: &[]u8, arg6: &[]u8, arg7: &[]u8,
) -> std::process::Error!i64
```

## 終了

`main` は `!std::process::ExitStatus` を返せます。プロセスの終了は整数 1 つでは
なく union 値 1 つで、`Success` / `Failure` を host の慣習的な code に、
`Specific(u8)` を明示した code に entry point が対応させます(ADR-0085)。
これで、正常終了したプログラムが platform の数字を名指さずに済みます。

`exit_code` は与えた code をそのまま返します。呼び出し側が意図した数字を
そのまま name するための形で、プロセスを終了させはしません。

## 引数と環境

`arg_count` は `--` 以降に渡された引数の数、`arg(index)` はその 1 つを返します。
範囲外の index は `ArgIndexOutOfBounds` です。`env` は環境変数の値、設定が無い
場合は `null` を返します —— 未設定は失敗ではないためです(docs/style.md)。host が
引けない名前(埋め込み NUL)も値を持ちません。

`executable_path` は走っている実行ファイルの path を caller-owned な `String`
として返すので、起動された directory に依存せず横に置かれたものを見つけられます。
返るのは kernel が報告する path そのままで、install tree との間に symlink がある
場合は [`std::fs::real_path`](fs.md) が解決します。

## 時刻

`monotonic_millis` は 1 つのプロセス内で event を順序づけるための単調な
ミリ秒です。`unix_millis` は Unix epoch からのミリ秒で、他のプロセスや他の
マシンと合意できる瞬間を名指します —— artifact の横に書く timestamp はこちらです。
どちらの読みも数のままで、[`std::time`](time.md) の `instant` / `unix` が時計ごとの
型を付けます。

## 子プロセス

`spawn_wait8` は子プロセスを起動して終了を待ち、その exit status を返します。
引数は最大 8 つ(実行ファイルとその引数 7 つ)の固定並びで、`argc` が実際に
使う数です。可変長の引数列は、それを渡す言語側の形が決まってから扱います。

`wasm32-wasi` の WASI preview1 host boundary は子プロセスを提供しません。
`spawn_wait8` に到達する program は build 時に target 非対応として拒否します。
到達しない helper は module から除かれるため、それだけで host capability を要求しません。

`wasm32-browser` は process を持つように偽装しません。`std::process` の API に到達する
program はすべて build 時に target 非対応として拒否します。browser entry が返す status は
`std::process` capability ではなく、`main` と host の compiler-defined boundary です。

`std::process::Error` は `InvalidArgumentCount`、`MissingExecutable`、
`OutOfMemory`、`ArgIndexOutOfBounds`、`ExecutablePathUnknown` を持ちます。
