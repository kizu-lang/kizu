# ADR-0085: `main` は exit を `ExitStatus` で返し、容量の増加戦略は実装が決める

Status: 採用

## 背景

`run` を build 実行に切り替えたとき(ADR-0083)、interpreter と native で答えが
食い違う example が 6 本見つかった。そのうち 2 本は**バグではなく、仕様が決まって
いなかった**ものだった。conformance が interpreter の実装詳細を期待値として
固定していたために、食い違いとして現れていた。

| example | interpreter | native |
| --- | --- | --- |
| `std_array.kizu` | `capacity()` が 2 | 4 |
| `error_union_try.kizu` | exit 0 | exit 2 |

## 決定 1: `main` は `void`、`<E>!void`、または `<E>!std::process::ExitStatus` を返す

`fn main() -> !i64` のような整数返しは禁止する。checker が拒否する。

```text
type error: `main` returns `!i64`, expected `void`, `!void` or `!std::process::ExitStatus`
```

理由は移植性である。exit status は platform ごとに形が違い、整数 1 つでは表せない。
Zig は `u8` / `!u8` を main の戻り値型から**外し**、`std.process.ExitStatus` union
に置き換えた(ziglang/zig#16135。plan9 は文字列、UEFI は `std.os.uefi.Status`)。
Rust も `i32` を返す形は持たず、`Termination` trait と `ExitCode` 型を使う。整数を
exit status にするのは C の形であり、両言語とも意識的に避けている。

明示的な exit status は Zig と同じ union で返す。selfhost CLI の `run` / `test` が
子 process の exit status を転送する必要が出たときに設計した。

```kizu
pub union ExitStatus {
    Success,
    Failure,
    Specific(u8),
}
```

hosted native では `Success` は 0、`Failure` は 1、`Specific(code)` はその code に
写る。plan9 / UEFI のような platform 固有の形は、その target を持つときに
`Specific` の payload を comptime で切り替える(Zig の switch と同じ)。素の
`ExitStatus`(error union でない形)は Zig と同じく許さない。wasm backend は
error union の main を持つ program をまだ表せないので、この写しは native backend
だけが持つ。

error を返した main は、診断を出して exit 1 で終わる。これは Zig / Rust と同じ。

却下した案:

| 案 | 却下理由 |
| --- | --- |
| `main -> !i64` / `u8` の整数返し | platform ごとに形が違う exit status を整数 1 つに固定する C の形。Zig / Rust とも避けている |
| `std::process::exit(code)` の noreturn 関数 | 隠れた制御フロー(どこからでも process が消える)を入れる。値を `main` まで返す形なら制御フローが見える |
| conformance の directive に exit code 表記を足す | `-fails` 系は「error: を含む出力 + 非ゼロ」の promise で足りている。具体 code の契約は CLI test が持つ |

## 決定 2: 容量の増加戦略は実装が決める

`capacity()` について SPEC が保証するのは `capacity() >= len()` だけとする。
特定の値に依存する example は書けない。

Rust の `Vec` は増加戦略を保証していないと明記しており、Zig も同様に実装詳細
として扱っている。conformance が値を固定すると、実装を差し替えられなくなる。

実装は Zig の `ArrayList.growCapacity` に合わせる。

```c
static int64_t kizu_array_grow_capacity(int64_t minimum, int64_t elem_size) {
    return minimum + minimum / 2 + kizu_array_init_capacity(elem_size);
}
```

1.5 倍を選ぶのは、黄金比未満の増加率であれば以前に解放した領域を後の確保で
再利用できるためである。2 倍では等比級数の性質上それができない。Rust 自身が
rust-lang/rust#111307 で 2 倍を最適でないと認めている。

初期容量は 1 cache line に収まる要素数(最低 1)とする。`i64` なら 8、`u8` なら
64 になる。小さい container が cache line 未満の確保から始まるのを避ける。

## 影響

- `examples/error_union_try.kizu` は `!void` を返し、値を `print` する形になった。
  この example が確かめたいのは `try` の伝播であり、意味は変わらない。
- `examples/std_array.kizu` / `std_string.kizu` は `capacity()` の値ではなく
  `capacity() >= len()` を検証する。
- `examples/std_string_storage_boundary.kizu` は `clear` の前後で capacity が
  変わらないことを検証する形にした。SPEC が保証しているのはそちらであり、
  以前の `== 16` は実装詳細だった。
- `examples/negative/main_returns_value.kizu` を追加した。
- pending が 2 件減った。
