# Kizu 性能の静的監査（2026-11-08, base: main 1a4a1d42）

目的は、計算量とメモリの形で Rust / Zig / Go の同種 compiler より構造的に劣る
場所を特定すること。実ベンチは別環境の最終確認段で行う前提で、ここでは
コード読解と、小さな合成入力による n / 2n / 4n の比で裏取りした。

## 0. 前提と測り方

- 監査範囲: Go seed（`internal/`, `cmd/kizu`）と shipping 実装（`compiler/src/`）の
  front end / checker / ownership / IR / LLVM・wasm backend / native / cache / CI。
- **測れたのは Go seed だけ**。この環境には clang が無いため、selfhost compiler の
  build も native link も実行できない。`~/.local/bin/kizu` も Go seed（`runtime.gopanic`
  を含む）だった。下で「Kizu 側」とした項目は、すべてコード読解による確認。
- 計測には、`/tmp` に作った rusage ラッパー（user / sys / wall / maxrss）を使った。
  profile は、ツリーを `/tmp` に複製して `cmd/kizu/main.go` に pprof を足した binary で
  取った。監査対象の worktree は変更していない。各点 1 回の計測で、4 core のマシン。
  比を見るための数値で、絶対値は保証しない。
- 合成入力は `/tmp/kizu-perf/gen/{gen,gen2}.py` で作った（形は各項目に書く）。
- 表記
  - **[測定]**: n 倍のスケーリングで裏取りしたもの
  - **[確認]**: 呼び出し連鎖を読んで確定したもの
  - **[推測]**: 規模や効果を見積もっただけのもの
  - **要仕様判断**: 意味論や約束を変えるもの

### 全体の実測（Go seed）

| 入力 | user | maxrss | 備考 |
| --- | --- | --- | --- |
| `check examples/hello.kizu` | 0.10s | 12MB | うち約 60ms が std 全体の parse（§1-2） |
| `check compiler`（Kizu 142k 行 + std 28k 行） | 7.3s | 472MB | LLVM emit まで走る（§1-10） |
| `build --emit-llvm compiler` | 7.7s | 474MB | 出力は **59MB / 77 万行**の `.ll` 1 本 |
| `build --emit-llvm --opt compiler` | 10.2s | 687MB | |

`check compiler` の CPU sample（8.07s）の内訳:

| 処理 | 割合 |
| --- | --- |
| `llvm.emit` | 29%（うち自己検証 `verifyEmittedText` 5.8%） |
| `ir.LowerForTarget` | 16%（うち `ir.verifyWithTypes` 5.9%） |
| ownership | 8% |
| project load / parse | 6% |
| types | 3% |
| GC mark | 約 30%（割り当ての多さ） |

seed では checker は安く、**backend と割り当て**が支配的。Kizu 側は計測できていない。

参考までに、`compiler.ll` の命令構成は次のとおり。
- `extractvalue` 134k / `insertvalue` 67k / `alloca` 40k / `call` 81k
- 第一級 aggregate を多用しているので、clang -O0 の FastISel が SelectionDAG に
  fallback しやすい形をしている（[推測]、clang の時間は未測定）。

---

## 1. 優先順位付きの候補一覧

順位は、影響（大規模入力での効き）× 直しやすさ × 意味論リスクの低さで付けた。
「両」は Go と `compiler/` の両方に同じ変更が要るもの、「K」は Kizu 側だけを Go の
計算量に揃えれば済むもの（出力は変わらない）。

| # | 候補 | 形 | 根拠 | 実装 | リスク |
| --- | --- | --- | --- | --- | --- |
| 1 | Kizu 移植で、Go の map が線形 Array に置き換わっている | O(M²) / O(V²) | 確認 | K | 無 |
| 2 | error set の番号付けのためだけに std 全体（28k 行）を parse している | 毎コマンドの固定費 | 測定 | 両 | 無 |
| 3 | Kizu の SHA-256 が表引きと除算のソフト実装で、warm run のたびに走る | 大定数 × (IR + 208KB) | 確認 | K | 無 |
| 4 | ownership の last-use 計算がブロックの入れ子ごとに部分木を歩き直す | O(d³) | 測定 | 両 | 無 |
| 5 | 文ごとの borrow 解放の全走査と、IR `env.remove` の O(n) | O(n²) | 測定 | 両 | 無 |
| 6 | メソッド receiver の二重読みと、`isPlainDataType` のメモ無し | **O(2^k)** | 測定 | 両 | 無 |
| 7 | view の bounds check が `--opt` でも消えない（退行） | 生成コード | 観測 | 両 | 無 |
| 8 | `try` ごとの全 binding 走査 | O(n²) | 測定 | 両 | 無 |
| 9 | inline recursion 検査が struct 連鎖について二乗、DeinitOwners の不動点を 7 回 | O(S²) | 測定 / 確認 | 両 | 無 |
| 10 | `check` が LLVM emit まで走る（check 時間の約 3 割） | 定数倍 | 測定 | 両 | **要判断** |
| 11 | 自己検証（IR verify を 2〜3 回、LLVM text verify）と dominance の O(depth) | 定数倍 + O(n²) | 測定 / 確認 | 両 | 低 |
| 12 | 実行ファイルの cache key が IR 全文、単一翻訳単位、incremental 無し | 構造 | 確認 | 両 | **要判断** |
| 13 | `[N]T` の値を index するたびに配列全体を store / memcpy する | 生成コード O(N)/iter | 観測 | 両 | 無 |
| 14 | 型検査の結果を後段が使わず、ownership / IR が型を再導出する | 定数倍 2〜3 | 確認 | 両 | 中（大改修） |
| 15 | 関数・宣言の属性不足（`nounwind`、panic の `noreturn cold`、borrow の `nonnull` 等） | 生成コード | 観測 | 両 | `noalias` のみ要判断 |
| 16 | LLVM text の後処理を関数ごとに 5〜6 段（Kizu は no-op でもコピー） | 線形 × 6〜8 | 確認 | 両 | 低 |
| 17 | 型の綴りの文字列往復、qualify の AST 全複製、Go lexer の `[]rune` | 定数倍 / メモリ 2 倍 | 確認 | 両 / Go | 低 |
| 18 | wasm: `usesBuiltinCall` を約 60〜100 回、WAT → 再 parse → binary、rotate / version が O(n²〜n³) | O(K·I) | 確認 | 両 | 低 |
| 19 | build cache が FIFO（LRU でない）、miss ごとに全 entry を読む、docs が古い | 運用 | 測定 / 確認 | 両 | 無 |
| 20 | CI cache の key 設計（per-sha 保存、selfhost key に `*_test.go` を含む） | CI 時間 / 容量 | 確認 | CI | 無 |
| 21 | Kizu の arena が関数をまたいで解放されない（ScopeStore / EnvStore / TypeTree） | メモリ O(総 clone) | 確認 | K | 無 |
| 22 | Kizu の native が warm run でも毎回 `/bin/uname` を spawn する | 固定費 | 確認 | K | 低 |

### 実コードへの露出（指数・三乗の形が今どれだけ踏まれているか）

compiler/ と std を走査して数えた。
- else-if 連鎖は最長 6（`runtime_source.kizu` の C 文字列内）
- インデントは最大 15 段
- 1 行のメソッド連鎖 `).m(` は最大 1
- 1 関数の let / var は最大 309（`ast_test.kizu`）

したがって **#4・#6 は今の self-build では潜在的**で、利用者のコード（生成コード、
builder 連鎖、大きな dispatch）で初めて病的になる。**self-build に今効くのは
#1・#2・#3・#10〜#12・#14・#16 の定数倍と、Kizu 側の線形探索**だと見ている。

---

## 2. 詳細

### #1 Kizu 移植で、Go の map が線形 Array になっている [確認, K]

ADR-0130 の機械移植で、Go 側の map や `collectBindings` が「Array を `while get(index)`
で線形探索する」形に置き換わった箇所がある。出力は同じでも計算量が 1 段上がっている。

1. **ownership の `record_method_callee`**
   - 場所: `compiler/src/internal/ownership/method.kizu:73-83`
   - メソッド呼び出しを解決するたびに `result.method_callees` 全体を走査して重複を
     確認する。Go は `map[*ast.FieldExpr][]string`（`internal/ownership/result.go:52`）。
   - **プログラム全体で O(M²)**。M は検査した全メソッド呼び出しで、compiler/ と std の
     `.m(` は約 3.4 万箇所ある（M²/2 ≈ 5.7 億回の比較、[推測]）。
2. **IR 側の読み出し**
   - 場所: `compiler/src/internal/ir/slots.kizu:679-708`
     （`resolved_method_call` / `resolved_method_signature_at`）
   - 同じ Array を呼び出しごとに線形に引くので、ここも O(M²)。
   - `retired_for_return`（`lower.kizu:764`）と `function_pointer_mut_borrows`
     （`ownership/call.kizu:176`）も同じ形。
3. **ownership の分岐 merge**
   - 場所: `binding.kizu:262`（`find_by_id`）、`binding.kizu:621-628`（`merge_moved_from`）、
     `control.kizu:426-427, 470-471, 635, 664, 1330`
   - Go は id → binding の map を 1 回作る（`checker.go:10241`）。Kizu は連鎖を線形走査
     するので、if 1 つあたり O(V²)。
4. **IR の env**
   - 場所: `compiler/src/internal/ir/env.kizu:49-109`
   - `get` / `bind` / `unbind` がすべて線形で、識別子を 1 回読むたびに O(locals)。
   - `merge_envs`（`control.kizu:573-580`）は if 1 つあたり O(m²)、`merge_match_envs`
     （`match.kizu:516-546`）は O(m² × arm 数)。Go は `map` + `order`（`internal/ir/env.go`）。

- 直し方: node id / name id をキーにした `map::Map<i64, …>` を持つ（Go と同じ構造に
  戻すだけ）。比較は既に interned id なので、置き換えは機械的にできる。
- 規模: 小さいプログラムでは見えない。self-build では M が大きいので、Kizu 製 compiler
  が自分を build するときに最も効く候補（[推測]、selfhost の profile が取れれば最初に
  見るべき箇所）。
- 二重実装: Kizu 側だけの修正で、出力は不変。corpus と bootstrap の一致がそのまま
  判定になる。

### #2 std 全体を parse するのは error set の番号付けのためだけ [測定, 両]

- 場所
  - Go: `internal/project/std.go:245-274`（`readStdErrorSets`）。全 module の全ファイルを
    `parseModuleFile` する。呼び出し元は `internal/ir/lower.go` の `lowerErrorSet` と
    `cmd/kizu/build.go:44`。
  - Kizu: `compiler/src/internal/project/std.kizu:471-549`。lowering のたびに無条件に走り、
    `read_std_graph()` も 2 回目が走る。
- 測定: `check hello.kizu` の profile（100ms）で、`StdErrorSets → parseModuleFile` が
  60ms を占めた。hello が実際に必要な std は 1.8k 行（`std::fmt` の閉包）なのに、
  28k 行 / 1MB を lex・parse している。
  - 補足: `docs/architecture.md:16` の「std 約 9k 行」は古い。
- 欲しいのは `error Name { a, b }` の member 名と、宣言の順序だけ。
- 直し方:
  - token 列だけを走査して top-level の `error` 宣言を拾う（`scanStdPaths` が std の
    全ファイルを既に lex しているので、そこで同時に集めれば追加の I/O も無い）。
  - 番号の決め方（module-path 順 → file 順 → 宣言順）は変えないので、runtime の
    `#define` も cache key も不変。
- 関連 [確認]: `import std;` を書くと全 module を取り込み、`scanStdPaths` の lex と
  parse の lex で std を 2 回 lex する（Go `std.go:177-195`、Kizu `std.kizu:287-328`）。

### #3 Kizu の build cache 用 SHA-256 がソフト実装 [確認, K]

- 場所: `compiler/src/internal/sha256/sha256.kizu`
  - 冒頭のコメント（5-11 行）は「Kizu has no … bitwise operators yet」。
  - AND / XOR は 65536 要素の表を引く（`lookup32`, 189-199 行）。
  - rotate は除算と剰余（`rotr`, 210 行）。
- このコメントはもう事実ではない。`lib/kizu/std/src/crypto/sha256.kizu:127,167` は
  `u32` と `>>` `^` `|` で書かれている。
- 経路: `buildcache::new` で表を作る（13 万回の append）。cache が hit する `run` でも、
  runtime C source（約 208KB）と LLVM IR 全文を hash する。
- 規模: docs が warm run の桁を約 10ms としている経路なので、固定費の大部分になり得る
  （[推測]、clang が無く selfhost を測れない）。
- 直し方: `std::crypto::sha256` に差し替える。出力は FIPS 180-4 で同じなので、Go と
  共有している cache の key も一致したまま。

### #4 ownership の last-use 計算が入れ子について三乗 [測定, 両]

- 場所
  - Go: `internal/ownership/checker.go:9977`（`blockLastUses`）、
    `:10006-10075`（`stmtIdentUses` / `blockIdentUses`）
  - Kizu: `compiler/src/internal/ownership/uses.kizu:15, 119`
- 形
  - `checkBlock`（`checker.go:798`）が、入れ子の各段で自分の部分木を全部歩く。
  - さらに子の識別子リストを `append(uses, child...)` で親へ毎段コピーするので、
    深さ d に対して O(d³)。
  - parser は `else if` を「else ブロックの中の if」にする（`parser.go:1067`）ので、
    **長い else-if 連鎖も同じ形になる**。
- 測定（`check`, user 時間）

  | 入力 | n | user |
  | --- | --- | --- |
  | 入れ子の if | 125 / 250 / 500 / 1000 / 2000 | 0.18 / 0.63 / 3.9 / 26.7 / 198s |
  | else-if 連鎖 | 100 / 200 / 400 / 800 | 0.13 / 0.30 / 1.38 / 9.3s |

  - 入れ子の if は 2 倍ごとに約 7 倍。
  - else-if 連鎖は 2 倍ごとに約 6.8 倍で、pprof では `stmtIdentUses` が 37%。
- 規模: 今の compiler/ では潜在的（連鎖 ≤ 6）。生成コードや大きな dispatch では秒〜分の
  単位になる。Rust の NLL は liveness を MIR 上で 1 回のデータフローとして求めるので、
  この形にはならない。
- 直し方: 関数ごとに 1 回、文 id → 最終使用位置を前計算する（または、名前ごとの
  最終位置を map に直接書き込む再帰にして、リストの連結をやめる）。

### #5 文ごとの borrow 解放の全走査 / IR `env.remove` [測定, 両]

- 場所
  - ownership: `checker.go:9840`（`releaseLastUseBorrows`、文ごとに `s.values` 全体を
    map 走査）、Kizu `uses.kizu:250-259`
  - IR: `internal/ir/env.go:41-53`（`remove` が毎回 `order` を確保し直す）。呼ぶのは
    `lower.go:1279-1290` の scope 終了処理。Kizu は `env.kizu:85-109`。
- 測定: 1 関数に `let` を n 個並べた入力で、n = 2k / 4k / 8k / 16k のとき
  0.24 / 0.54 / 1.96 / 6.7s（2 倍ごとに約 3.5 倍）。
  - pprof では `releaseLastUseBorrows` が 48%、`env.remove` が 17%。
  - 比較として、n 関数の入力（1.0s @16k）と n 個の逐次 if（1.6s @16k）は線形だった。
- 直し方
  - borrowTargets を持つ binding だけのリストを持つ。
  - scope は LIFO なので、remove は末尾から pop する（退避した順の逆に消せば常に末尾）。
- 規模: 1 関数に数千の let は稀。self-build の最大 309 では無視できる。

### #6 指数時間になる 2 箇所 [測定, 両]

**(a) メソッド receiver の二重読み**
- 場所
  - Go: `internal/ownership/checker.go` の `checkMethodCallExpr`。`checkBoxReceiverExpr`
    （:8555）と `checkCopyTemporaryReceiverMethod`（:8303）が、同じ receiver を
    それぞれ `readExpr` する。
  - Kizu: `ownership/method.kizu:109,115,140,416`
- 測定: `unit().add(unit())…` で `.add` を k 個並べた入力。k = 14 / 16 / 17 / 18 / 19 のとき
  0.15 / 0.30 / 0.47 / 0.90 / 1.70s で、**k が 1 増えるごとに 1.9 倍**。
  pprof では `checkBoxReceiverExpr` が 78%。
- 直し方: receiver の型を 1 回だけ求めて、各判定に渡す。

**(b) `isPlainDataType` / `isPlainDataAggregate` にメモが無い**
- 場所
  - Go: `internal/types/checker.go:9583-9645`
  - Kizu: `types/type_collection.kizu:383-477`
  - ownership 側の同等品: `ownership/checker.go:9603`
- `seen` には再帰経路しか入らないので、`struct D{k} { a: D{k+1}, b: D{k+1} }` のように
  同じ型を 2 回持つ DAG で O(2^d) になる。`Array.get` / `clone` / `Map.get` などで、
  式 1 つごとに呼ばれる。
- 測定: d = 16 / 18 / 20 のとき 0.33 / 1.08 / 3.99s で、2 段ごとに約 3.5 倍。
  pprof では `isPlainDataAggregate` が 49%。
- 直し方: 型 → bool のメモを Checker に持たせる。宣言が出揃った後は純関数なので安全。

### #7 view の bounds check が BCE でも versioning でも消えない [観測, 両]

- 原因
  - 49d0aee9（2026-09-18 の perf コミット）で、index の検査が
    `cast→u64` 後の `binary.>=` 1 本になった（`internal/ir/lower.go:4051`
    `checkIndexInRange`）。
  - ところが BCE（`internal/ir/bounds.go:336, 419-440`）と versioning
    （`internal/ir/version.go:330-352`）は、i64 の比較形しか照合していない。
- 観測（`kizu ir --opt`、`for 0..mem::count<i64>(xs) |i| { s = s + xs[i]; }`）:
  ```
  %8: u64 = cast %5: i64, u64
  %9: u64 = cast %in1.1: i64, u64
  %10: bool = binary.>= %8: u64, %9: u64
  cond_fail %10: bool, bounds(%5: i64, %in1.1: i64)
  ```
  定数長の `[8]i64` と `for 0..8` でも `icmp uge i64 %i, 8` が残った（LLVM IR で確認）。
- 影響
  - wasm では LLVM の後始末が無いので、hot loop の check がそのまま残る。
  - native は LLVM の IndVars / ConstraintElimination が `slt` のガードから消すと
    見ている（[推測]、clang 無しで未確認）。
  - Rust / Zig ReleaseSafe ならこの形は確実に消える。
- 直し方: `cast` を辿って元の i64 を取り出し、`nonNegative(i) && less(i, len)` が
  成り立つなら削除する。残した check からは `0 ≤ i < len` を学習させる
  （Kizu 側は `bounds.kizu:793-830`, `version.kizu`）。
- 退行の恐れが強いので、behavior test に「この形では check が残らない」ことを実行結果で
  示す手段が無いか検討したい。ただし構造 pin（生成テキストを grep で固定）は禁止なので、
  例えば wasm の命令数や実行時間の比で示せるか、が論点になる。

### #8 `try` ごとの全 binding 走査 [測定, 両]

- 場所
  - Go: `ownership/checker.go` の `readTryExpr` → `checkOwnersConsumed`（:1297、全 binding ×
    `errDeferCoversID` の線形）と `validateErrDeferReceivers`（:1146）
  - Kizu: `block.kizu:473, 616-639`
- 測定: `let x{k} = try p();` を n 個並べた入力で、n = 1k / 2k / 4k のとき
  0.22 / 0.42 / 1.29s。pprof では `checkOwnersConsumed` が 40%。
- compiler/ 自身が try を非常に多く使うので、関数が大きいほど効く。
- 直し方: 「consume 義務が残っている owner の id 集合」を増分で維持する。

### #9 型宣言のグラフに対する二乗 [測定 / 確認, 両]

- **`checkInlineRecursion`**
  - 場所: Go `internal/types/recursion.go:14-63`、Kizu `types/recursion.kizu:16-131`
  - struct ごとに新しい `seen` で DFS し、辺ごとにラベル文字列を作る。
  - 測定: `struct S{k} { a: S{k+1} }` の連鎖で、n = 500 / 1k / 2k のとき
    0.21 / 0.47 / 1.54s。pprof では `inlinePathTo` が 69%。
  - 直し方: SCC か 3 色 DFS で、全体を 1 回で O(V+E) にする。経路の文字列は循環を
    見つけたときだけ作る。
- **`DeinitOwners`**
  - 場所: Go `internal/ast/deinit_owners.go:29-39`、Kizu `ast.kizu:2213-2242`
  - 全 decl を不動点まで回し、field ごとに `typ.Parse` / `String` する。それを
    types / ownership / IR / derive がそれぞれ呼ぶので、1 compile で 7 回走る。
  - [確認]。`check compiler` での実測は 0.5% と小さい。
  - 直し方: project が 1 回だけ計算して Program に載せる。逆辺を作って worklist で
    伝播させる。

### #10 `check` が LLVM emit まで走る [測定, 要仕様判断]

- 場所: `cmd/kizu/check.go:19-30`（2f7eb622 / #1622）
- 「`check: ok` は backend が受理する約束」という設計判断による。
- `check compiler` の 29%（約 2.1s）が `llvm.emit`。pre-commit の `kizu check compiler` が
  commit ごとにこれを払っている。
- 比較: `cargo check` は codegen を、`zig build-exe -fno-emit-bin` は emit を飛ばす。
- 選択肢（ユーザー判断）
  - (a) 現状維持
  - (b) `check` は IR verify までにし、emit の検査は `test` / `build` / conformance に任せる
  - (c) emit は行うが、text を組み立てずに検証だけする経路 —— 原理 9（経路 1 本）に
    抵触しない形にできるかの検討が要る
- 意味論を変えるので、ここでは提案に留める。

### #11 自己検証の重複 [測定 / 確認, 両]

- 測定: `ir.verifyWithTypes` 5.9% + `llvm.verifyEmittedText` 5.8% で、毎回 約 12%。
- 重複している箇所
  - package build では `addPackageMain`（`cmd/kizu/load.go:218`、Kizu `main.kizu:1726`）が、
    1 block の main を足しただけで **module 全体を再 verify** する。
  - `--opt` ではさらに `optimize.go:26` で 1 回、計 2〜3 回になる。
  - 関数 1 つの中でも、`phiIncomingDominates` と `valuesDominateUses` がそれぞれ
    dominator tree を作る（`verify.go:344-345, 384-385`）。
  - `dominates` は idom の鎖を辿るので O(depth)（`verify.go:568-585`）。逐次 if や
    k arm の match では O(n²)。
- 直し方
  - 追加した main だけを verify する。
  - tree を 1 回だけ作り、pre / post 番号で `dominates` を O(1) にする。
- 検証を外す提案ではない。重複を除くだけなので、保証は変わらない。

### #12 build の再利用単位が「プログラム全体」 [確認, 要仕様判断（設計）]

- 実行ファイルの cache key は LLVM IR の全文（`internal/native/build.go:107-110`）。
  - 1 関数の変更で、`.ll` 全体（self-build で 59MB）を clang が 1 プロセス・1 スレッドで
    compile し直す（`build.go:130-141, 422-446`）。
  - front end も毎回全体を通る（hit 判定のために IR が必要）。
- 比較
  - rustc: crate 単位 + incremental + codegen unit 分割（debug は既定 256）
  - Zig: incremental + 並列 codegen
  - Go: package 単位の export data + object cache
- 良い点 [確認]
  - IR に source 位置が入らない（`hello.ll` を確認）ので、コメントだけの編集は hit する。
  - runtime object は clang の版を key に入れて 1 回だけ compile している。
  - 一時 `.ll` は `RemoveAll` で残らない。
- `docs/perf.md` の「cache 設計の制約」は module-aware key（manifest / module graph /
  public interface hash）を掲げているが、実装はまだ無い。
- 方向（ユーザー判断）
  - (1) module 単位の object 分割: `internal` linkage と inline 境界、-O3 での cross-module
    inline（ThinLTO）が論点。
  - (2) 関数単位の IR hash で object を cache する。
  - どちらも「雑な並列化」ではなく再利用単位の設計だが、生成物の同一性（bootstrap の
    byte 一致）への影響が大きい。

### #13 `[N]T` の値を index するたびに配列全体を store する [観測, 両]

- 場所: `internal/ir/lower.go:3152-3167`（`lowerArrayStorage`）、
  `internal/llvm/emit.go:2953`（`writeLocalSlot`）
- 観測（`--opt` の `.ll`、`fn total(xs: [8]i64)`）: loop body で毎 iteration
  `store [8 x i64] %kizu.xs, ptr %kizu.7` してから GEP で読んでいる。引数は既に
  `ptr noalias nocapture readonly %kizu.xs.addr` で渡ってきている。
- 影響: N が大きいと 1 iteration ごとに O(N)。LLVM が消せるかは src が alloca か引数かで
  変わる（[推測]）。Rust は元のメモリを直接 index する。
- 直し方: 不変な値は既存の記憶域を使う（引数は `.addr` を使い、`let` は束縛時に 1 回だけ
  slot を作る）。値は不変なので意味論は変わらない。

### #14 型検査の結果を後段が使わない [確認, 両, 大改修]

- `types.Check` は error しか返さない（`cmd/kizu/load.go:236`）。
- ownership は `readExpr` で型名を、IR は `lowerExpr` で `Value.Type` を、それぞれ
  導出し直す。式の型付け、generic instance の展開、`comptime for` の展開、型述語が
  3 pass で重複している。
- さらに IR は命令ごとに `resolveMetaTypeDeep` / `typ.SubstituteText` で型を parse し
  直す（`helpers.go:58-59`, `typ.go:806-823`）。
- 比較: rustc の typeck results、Go の `types.Info`、Zig の AIR。
- 直し方
  - 短期: (instance, 型名) → 解決結果のキャッシュ（IR）と、型述語のメモ化。
  - 本命: expr id → Type の表を types が出し、後段がそれを読む。両実装の 3 pass の API が
    変わる。

### #15 関数・宣言の属性 [観測, 両]

- 観測
  - `declare void @kizu_panic_bounds(...)` に `noreturn cold nounwind` が無い。
  - 定義関数の属性は probe-stack だけで、`nounwind` が無い。
  - borrow 引数（`&var Array`, `&[n]T`）は素の `ptr`。
  - slice は `{ptr, i64}` の値渡しなので、中の ptr に属性を付けられない。
  - `for` の step（`add i64 %i, 1`）に nuw / nsw が無い。
- 影響: FunctionAttrs が `nounwind` / `willreturn` を推論できないので、loop 内の pure
  helper を LICM / CSE しにくい。vectorizer は実行時の重なり検査を入れる。
- リスクの区別
  - リスク無し: `nounwind`（Kizu に unwind は無い）、panic の `noreturn cold`、borrow の
    `nonnull dereferenceable align`、compiler が作る `for 0..end` の step の `nuw nsw`
    （`i < end ≤ INT64_MAX` が保証されている）。ユーザーの `+` の wrap（SPEC §6.9.2）
    には触れない。
  - **要仕様判断**: `noalias`（`&var` の排他は SPEC §9 にあるが、unsafe 由来の borrow と
    std 内部でこの保証が破れないことの確認が要る）。
- 関連: runtime は `--opt` なら -O3 だが別 object で、LTO が無い。`kizu_map_get` や
  `kizu_map_len` などは inline されない C 呼び出しになる（`build.go:324, 422`）。

### #16 LLVM text の後処理 [確認, 両]

- 場所: Go `internal/llvm/emit.go:1164-1175`、`phi.go:180, 335`、`alloca.go:26`。
  Kizu `llvm.kizu:1238-1278`、`phi.kizu:416, 599`。
- 関数本体の text に対して次を順に行う。
  1. `resolvePhiPredecessors`
  2. `dropUnreachableTails`
  3. `unifyAggregateReturns`
  4. `splitAggregatePhis`（`strings.Split` で行に割って再 parse する）
  5. `hoistAllocasToEntry`（全関数で Split）
- Kizu は no-op の段でも、本体を新しい String に毎回 append する。出力全体を 6〜8 回
  コピー・走査している。
- 直し方
  - 短期: 変更があった段だけ書き出す。
  - 本筋: IR 段で phi / return を分ける（text の書き換えをやめる）。

### #17 文字列往復とコピー [確認]

- **型の綴りの往復**
  - parser が parse 済みの型を文字列に戻す（`parser.go:2069-2086` ほか）。
  - qualify が使用箇所ごとに `SplitArgs → Parse → MapNames → Text → Join` する
    （`qualify.go:736-759`, `resolve.go:214-236`）。
  - `typ.Table.Remember` が `String()` を 2 回呼ぶ（`types/type.go:27-31`）。
  - Kizu の `map_names` は言及ごとに新しい `TypeTree` arena を作る（`typ.kizu:285-289`）。
- **qualify の AST 全複製**: Go の `qualify.go` は全 node を `cp := *x` する。Kizu は新しい
  node を同じ arena に足す（`qualify.kizu:12-15`）。AST のメモリが約 2 倍になる。
  元の AST は再利用されないので、その場で書き換えられる。
- **Go lexer の `[]rune`**: `internal/lexer/lexer.go:85` が入力全体を `[]rune` にして
  （4 倍）、token ごとに `string([]rune)` を作る。Go 側だけで直せ、出力は不変。
- **GC**: seed の約 30% は GC で、上の割り当てが主な発生源と見ている（[推測]）。
  `GOGC=400` にすると user 7.3 → 5.4s、RSS 472 → 845MB。seed は shipping ではない
  ので優先度は低い。

### #18 wasm backend [確認, 両]

- `usesBuiltinCall`（Go `internal/wasm/process.go:29-46`、Kizu `process.kizu:24-50`）は
  1 回で module 全体を走査する。`target.go` や `fs.go` などから約 60〜100 回呼ばれるので
  O(K × 命令数)。1 回の走査で使用している builtin の集合を作れば済む。
- binary 出力は「WAT を書く → S 式として再 parse → encode」の 3 段（`emit.go:57`,
  `module.go:121-250`）。Kizu では opcode を `equal_bytes` の if 連鎖で引くので、
  命令 1 つで最大約 100 回の比較になる（`binary_instruction.kizu:174-255, 824`）。
- `--opt` の wasm 専用 pass
  - `RotateLoops`（`rotate.go:45-53, 297-321, 420-436`）は loop ごとに関数全体を走査し、
    逐次 loop n 個で O(n³)。
  - `VersionLoops`（`version.go:32-61`）は version を 1 つ作るたびに CFG 解析を作り直す。

### #19 build cache の運用 [測定 / 確認]

- **FIFO**: `enforceLimit` は `CreatedAt` の古い順に消す（`internal/buildcache/cache.go:236`）。
  hit したときに時刻を更新しないので、よく使う entry も古ければ消える（LRU にならない）。
  - 手元の cache は 770 entry / 248MB で、上限 256MiB に張り付いていた。
  - そのうち 18MB 級の compiler 規模の exe が 4 世代あった。
  - 直し方: hit 時に meta の mtime を更新し、それを基準にする。
- **miss ごとの全走査**: miss のたびに全 `*.json` を読んで sort する。770 entry の走査は
  warm で約 20ms、cold で約 470ms（`cache status` で測定）。conformance の cold run のように
  miss が数百回続くと、O(miss × entry) のファイル読みになる。
  - 直し方: 合計サイズを index ファイルに持つ、または上限を超えたときだけ走査する。
- **docs の不一致**: `docs/perf.md` の表は「LLVM IR text / wasm text を
  `build --emit-llvm` / `--target wasm32-wasi` で cache する」としているが、
  `GetOrBuildArtifact` の呼び出しは `native-exe` と `native-runtime.c` の 2 種類しか無い
  （`internal/native/build.go:107, 268`）。表を実装に合わせるべき。

### #20 CI の cache [確認]

- `.github/workflows/ci.yml`
  - `kizu-cache` の key は `…-${{ github.sha }}` で、restore-keys で直近を復元する。
    push ごとに新しい key になるので、毎回最大約 256MB を保存する（2 OS 分）。
    GitHub の repo cache 上限（10GB）を早く押し出し、selfhost cache も一緒に追い出される
    可能性がある（[推測]）。
  - `selfhost` の key は `cmd/kizu/**` と `internal/**` を hash するので、`*_test.go` だけの
    変更でも selfhost を作り直す（59MB の `.ll` を clang -O0 で compile）。
    `hashFiles` に `!**/*_test.go` を足せば、出力を変えずに外せる。
- selfhost は `build --target native`（-O0、`cmd/kizu/testmain_test.go`）で作られ、
  bootstrap の第 2 段もこの -O0 binary が走らせる。CI 時間の多くはここだと見ている
  （[推測]、計測は別環境で）。

### #21 Kizu の arena 保持 [確認, K]

- ownership の `ScopeStore` はプログラム全体で 1 つ（`ownership.kizu:638`）、IR の
  `EnvStore` は lowering 全体で 1 つ（`lower.kizu:466`）。clone した scope / env は
  関数をまたいで最後まで残る。
- types の `substitute_type_params`（`types.kizu:518-549`）は、呼び出しのたびに
  TypeTree を足して解放しない。
- RSS は総 clone 数 × locals、および総呼び出し数 × 型の大きさに比例する。Go は GC が
  回収するので、Kizu 側にしか無い問題。
- 直し方: arena を関数ごとに reset する。結果として残るものだけを外側の arena に置く。

### #22 Kizu の native が warm run でも `uname` を spawn する [確認, K]

- 場所: `compiler/src/main.kizu:2360` → `native.kizu:253-275`
- cache hit の `run` でも fork + exec が 1 回入る。Go は `runtime.GOOS/GOARCH` で済ませて
  いる。host は compile 時に `std::target` で決まる。

---

## 3. Kizu の設計上、残るのが正しいもの（劣化ではない）

- **ユーザー算術の wrap**（nsw 無し）と、shift の範囲検査（SPEC §6.9.2）。
- **2 つのコンテナを同じ index で読むときの、片側の bounds check**（`dot(a, b)`）。
  Rust / Zig も残す。native で versioning を有効にするかどうかは計測後の判断。
- **`[N]T` の値渡しの memcpy**: copy 型の意味論どおり（SPEC §7.1）。#13 は、渡した後の
  読み出しで毎回コピーするという別の問題。
- **heap 確保は Allocator を明示したものだけ**: 隠れた確保は見つからなかった。
- **`mustprogress` を付けない**: `while true` が定義された動作だから。
- **`try` の失敗側の `llvm.expect`**、bounds 失敗の `call; unreachable` の形（cold 推論が効く）。
- **generic を instance ごとに検査・lower すること**（ADR-0066）: Rust の monomorphize と
  同じ形。同じ instance はメモ化されている（`checkedInstances`）。

## 4. 問題が見つからなかったもの

- lexer / parser の lookahead は有界で、各種の index は map。
- 型検査器の scope は親リンク + map で、関数・型・メソッドの検索も map。
- `reachable.go` の閉包は BFS + map で線形。comptime の評価器も問題なし。
- LLVM の文字列定数は map で intern している。Kizu の出力 String は倍々で伸長するので、
  O(n²) の連結は無い。
- 到達可能性の刈り込みは効いている（`hello.ll` は 1.6KB で、使う関数だけが出る）。
- cache key に path や時刻が入らない。runtime object は 1 回だけ compile される。

## 5. 次の段（別環境）で測るべきもの

1. selfhost compiler の `check compiler` / `build compiler` の profile（`perf record`）。
   #1・#3・#21 の実際の割合は、ここでしか分からない。
2. clang -O0 / -O3 の時間を、59MB の `.ll` 1 本で測る（#12 の判断材料）。
   aggregate SSA の多さによる FastISel の fallback 率も併せて見る。
3. #7 の形を native -O3 で、LLVM が check を消すかどうか。
4. CI の 1 run の内訳（selfhost の build、bootstrap の第 2 段、conformance）。

裏取りに使った入力生成スクリプトと rusage ラッパーは `/tmp/kizu-perf/` にある
（リポジトリには入れていない）。
