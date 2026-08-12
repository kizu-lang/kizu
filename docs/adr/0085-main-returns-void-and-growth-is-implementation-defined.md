# ADR-0085: `main` は値を返さず、容量の増加戦略は実装が決める

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

## 決定 1: `main` は `void` または `<E>!void` を返す

`fn main() -> !i64` は禁止する。checker が拒否する。

```text
type error: `main` returns `!i64`, expected `void` or `!void`
```

理由は移植性である。exit status は platform ごとに形が違い、値 1 つでは表せない。
Zig は `u8` / `!u8` を main の戻り値型から**外し**、`std.process.ExitStatus` union
に置き換えた(plan9 は文字列、UEFI は `std.os.uefi.Status`)。Rust も `i32` を
返す形は持たず、`Termination` trait と `ExitCode` 型を使う。整数を exit status に
するのは C の形であり、両言語とも意識的に避けている。

error を返した main は、診断を出して非ゼロで終わる。これは Zig / Rust と同じ。

明示的な exit status が必要になったら、そのとき `ExitStatus` 相当を設計する。
Kizu は freestanding build も対象にしており、そこでは exit status の意味自体が
変わる。整数を今から仕様に埋めるのは早い。

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
