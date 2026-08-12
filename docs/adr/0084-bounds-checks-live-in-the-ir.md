# ADR-0084: 境界検査は IR に置き、backend は `cond_fail` を実装するだけにする

Status: 採用

## 背景

同じ「範囲外」が 4 通りに実装されていた。

| 経路 | 範囲外のとき |
| --- | --- |
| interpreter | Go の文字列 `runtime error: index out of bounds` |
| LLVM / slice syntax | `llvm.trap()` のみ。診断なし |
| LLVM / `Array` | `@.kizu.array.index_oob` を渡す別機構 |
| wasm | 検査そのものが無い |

原因は IR にあった。`slice.index` は「検査付き」という意味だけを持つ 1 命令で、
検査の実体は backend にあった。安全性の実現が backend ごとの実装品質に委ねられ、
LLVM は実装し、wasm はまだ、という状態が生まれる。

ADR-0083 が `run` と `build` を 1 本にしたのと同じ構造の問題が、その下で
もう一度起きていた。

## 他言語の扱い

いずれも中間表現に持っており、backend には置いていない。

| 言語 | 表現 | 形 |
| --- | --- | --- |
| Rust | MIR | `Assert` terminator。`BoundsCheck` だけ動的引数を持ち `panic_bounds_check` を呼ぶ |
| Swift | SIL | `cond_fail` 命令。**ブロックを分けない** |
| Go | SSA | 境界 panic の op。`runtime.panicIndex` を呼ぶ |
| Zig | AIR | build mode で消え、panic handler を差し替えられる |

Swift の `cond_fail` は制御フローを平坦に保ったまま失敗を表現でき、optimizer が
ループ外への巻き上げと複数検査のマージを行える。Rust は動的引数を入れた結果、
`usize` の整形コードがバイナリに入る問題を抱えている。

## 決定

### 1. IR に `cond_fail <cond>, "<message>"` を 1 命令追加する

terminator ではなく通常命令とし、ブロックを分割しない。

```
%3: i64  = slice.len %1: []u8
%4: bool = binary.< %2: i64, 0: i64
cond_fail %4: bool, "index out of bounds"
%5: bool = binary.>= %2: i64, %3: i64
cond_fail %5: bool, "index out of bounds"
%6: u8   = slice.index %1: []u8, %2: i64
```

### 2. 検査は lowering が生成する

`internal/ir` が `slice.index` と `slice.slice` の前に検査を置く。backend は
検査を書かない。`slice.index` は「検査済みの load」を意味する。

### 3. メッセージは静的文字列にする

`internal/diagnostic` の定数を interpreter と lowering が共有する。実際の index
と長さは含めない。Kizu は freestanding build を build policy に持っており、
Rust が踏んだ整形コードの混入を避ける。必要になれば `cond_fail.index` のような
専用形を後から足せる。

### 4. panic は runtime 関数にする

`kizu_panic(msg, len)` を呼ぶ。展開せず関数呼び出しにすることでコードサイズを
抑え、freestanding では別実装をリンクするだけで済む。

## 影響

- `internal/llvm` の `writeSliceIndex` / `writeSliceSlice` から検査生成が消えた。
  backend のコードは減っている。
- 同じメッセージは何度検査しても global 1 つで済む。
- wasm は `cond_fail` を実装するだけで安全になる。slice の検査を書き直す必要はない。
- `kizu ir` の出力を読めば、メモリを読む前に何を検査しているかが見える。
- conformance の `negative_slice_syntax_index_out_of_bounds` と
  `negative_slice_syntax_range_out_of_bounds` の `pending` を外した。

## まだやっていないこと

- `Array` の 6 種のメッセージ機構は `internal/llvm/array.go` に残っている。
  `get_or_panic` のような停止するものは `cond_fail` に寄せられる。optional を
  返す `get` は別の話であり、寄せない。
- 検査の巻き上げとマージは `ir.Optimize` に置ける。今は入れていない。
