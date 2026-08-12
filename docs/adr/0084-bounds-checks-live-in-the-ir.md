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

### 3. 失敗の種類と値を `cond_fail` が運び、文言は runtime に置く

```
cond_fail %4: bool, bounds(%2: i64, %3: i64)
```

失敗の種類と、その失敗が報告する値を命令が持つ。文言は runtime の C コードに
だけあり、IR にも backend にも診断文字列は無い。ADR-0072 の形が出せる。

```text
runtime error: index out of bounds
note: index is 3, length is 3
```

Rust の `AssertKind` と同じく、値を持つ失敗と持たない失敗を種類で区別する。
`Assert` が terminator なのに対し `cond_fail` は通常命令だが、どちらも比較は
別命令であり、検査と失敗を分けている点は同じである。分けているのは最適化の
ためで、比較が普通の値として見えていれば既存の畳み込みや共通部分式除去が効き、
Swift のようにループ外へ巻き上げられる。

初版では「静的文字列にして、Rust が踏んだ整形コードの混入を避ける」と判断したが、
これは誤りだった。Kizu の runtime は `kizu_print_int` で既に `printf` を
リンクしており、整数を出す費用は支払い済みである。Rust の問題は `core::fmt` が
丸ごと入ることで、専用の runtime 関数へ値を渡す形では起きない。

### 4. panic は種類ごとの runtime 関数にする

`kizu_panic_bounds(index, len)` のように、種類ごとに entry を持つ。展開せず
関数呼び出しにすることでコードサイズを抑え、freestanding では別実装をリンク
するだけで済む。

## 影響

- `internal/llvm` の `writeSliceIndex` / `writeSliceSlice` から検査生成が消えた。
  backend のコードは減っている。
- 診断文字列は Go 側から消えた。`internal/diagnostic` の runtime failure 定数も
  不要になり削除した。
- wasm は `cond_fail` を実装するだけで安全になる。slice の検査を書き直す必要はない。
- `kizu ir` の出力を読めば、メモリを読む前に何を検査しているかが見える。
- conformance の `negative_slice_syntax_index_out_of_bounds` と
  `negative_slice_syntax_range_out_of_bounds` の `pending` を外した。

## 失敗の 2 分類

その後 `Array` / `Map` / `Arena` も同じ整理に寄せた。分けるべきなのは「文言が
どこにあるか」ではなく、**失敗が停止するのか値として返るのか**だった。

| | 例 | 文言の置き場所 |
| --- | --- | --- |
| 停止する | 範囲外 index、`get_or_panic`、空 `Array` の pop、無効な arena handle、test の失敗 | runtime C |
| 値として返る | `Array.append` の `!void`、`Array.get` の `!T`、`Map.get` の `!T` | module global |

停止する失敗は、報告して終わるので runtime に文言を置ける。値として返る失敗は
`!T` の payload として Kizu コードに戻るため、module のデータとして存在しなければ
ならない。この 2 つを混ぜていたのが、同じ「範囲外」が 4 通りに実装されていた
理由である。

それぞれ 1 つのテーブルが持つ。

- `panicEntries`: 停止する失敗。key → runtime entry と引数型
- `failureValues`: 値として返る失敗。key → メッセージ

どちらも「モジュールが実際に使う分だけ」宣言する。以前は `Array` を使うだけで
5 つのメッセージ global が無条件に出ていた。

これで backend から `llvm.trap()` が消えた。停止する失敗はすべて診断を出す。

## 位置

`ir.Instr` が `Span` を持ち、失敗を報告する runtime entry は最後の 2 引数として
line と column を取る。ADR-0072 の形になった。

```text
runtime error: index out of bounds at 2:21
note: index is 3, length is 3
```

span を持たない箇所は line に 0 を渡し、runtime が位置を省略する。ADR-0072 が
「`at <line>:<column>` は primary span を持つ diagnostic に付ける」としている
通りである。

期待値と実値を持つ失敗は、ADR-0072 に従って **summary に** 書く。

```text
runtime error: expected 4, got 3
```

`note:` は「なぜそう判断されたか」に使う。範囲外なら index と length がそれに
あたる。

## まだやっていないこと

- 検査の巻き上げとマージは `ir.Optimize` に置ける。今は入れていない。
- span を持つのは `ast.IndexExpr` だけである。`Array` の method 失敗や
  `std::testing` の失敗はまだ位置を出せない。`ast.CallExpr` と `ast.FieldExpr`
  が span を持てば、同じ経路で出るようになる。
