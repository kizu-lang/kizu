# Self-Host Migration Readiness

この文書は、Go compiler から Kizu self-host compiler へ file/module 単位で移植する前に
固めるべき仕様と実装条件を整理する。

ここに書く内容は Kizu の思想に基づく推奨判断であり、最終判断は対応 Issue の
受け入れ条件で行う。

## 判断方針

優先順位は次の通り。

1. safe Kizu のメモリ安全性
2. ownership、borrow、cleanup、concurrency boundary の明確さ
3. failure と diagnostic の明示性
4. allocation、I/O、runtime cost の可視性
5. build time、cache size、CI 負荷の測定可能性
6. compiler 実装の単純さ
7. 書きやすさ。ただし上記を隠さない場合だけ

## 現時点の推奨

Go compiler を完成させ切ってから self-host に移るのではなく、self-host 移植に必要な
仕様境界を component ごとに固定する。

```text
仕様を固定する
Go compiler に oracle と検査を作る
Kizu module に移植する
Go/Kizu oracle を比較する
置き換え可否を判断する
```

## 移植前に固める項目

| 項目 | 推奨判断 | 移植への影響 |
| --- | --- | --- |
| module graph | `kizu.toml` と explicit import を正にする | selfhost/src を production 形状にできる |
| cross-module 型参照 | `token::Token` のような参照を先に Go compiler で完成させる | lexer の戻り値や parser の入力型に必要 |
| visibility | `pub` だけを v0.3 対象にする | alias/re-export を増やさずに進める |
| diagnostics | file/span/related span を stable oracle にする | self-host の error 表示を比較できる |
| `!T` | recoverable failure と diagnostic が必要な処理に使う | lex/parse/fs/allocation failure に必要 |
| `?T` | absence だけに使う | lookup miss と failure を混ぜない |
| `[]const u8` | source text と literal view の基本型にする | primitive `string` を復活させない |
| `std::array::Array<T>` | token/AST list の基本 container にする | allocator/deinit と borrow rule の検証が必要 |
| `std::map::Map<K, V>` | resolver/symbol table までに固める | lexer では blocker ではない |
| `std::testing` | component test で使う | Kizu 側 test を Go test 依存だけにしない |
| `kizu test` | package/component test を実行できるようにする | self-host compiler 開発体験の前提 |
| legacy `frontend.kizu` | oracle harness として freeze | production logic は selfhost/src に移す |

## Component Readiness

### token / lexer

推奨: 次に進めてよい。ただし Kizu lexer が `selfhost::token` を実際に参照し、
Go lexer と token kind、literal、byte span、line、column を比較できること。

blocker:

- cross-module 型参照が不十分な場合は Go compiler 側を先に直す
- `Array<Token>` が必要になった時点で allocator/deinit/borrow rule を検査する

### AST / parser

推奨: token/lexer の oracle が通ってから進める。

blocker:

- recursive AST shape
- `Array<Node>` または Arena/Handle の選択
- parse error の `!T` diagnostic
- parser snapshot の粒度

### diagnostics / resolver

推奨: module graph と visibility diagnostic を Go compiler 側で強化してから進める。

blocker:

- import cycle diagnostic
- private access diagnostic
- symbol table に必要な `std::map::Map<K, V>`

### types / ownership

推奨: parser/resolver 後。memory-safety の中核なので、negative conformance が十分に
揃うまで production path へ切り替えない。

blocker:

- field borrow
- mutable borrow
- last-use borrow end
- array element borrow
- cleanup/deinit boundary
- concurrency capability boundary

### IR / backend / cache

推奨: Go-owned oracle を維持する。backend と cache は self-host switch decision として
別 issue で扱う。

blocker:

- backend smoke fingerprint
- cache key ownership
- no-op rebuild / source edit / std hash measurement

## Current Bootstrap Evidence

最終更新: `feat/selfhost-bootstrap-chain` で selfhost manifest を stage input graph に含め、
parser/checker/lower summary と parse / token / illegal-token component metrics を Kizu struct
で渡し、selfhost lexer に cursor-carrying `TokenScan` を追加した working tree 時点。

この記録は現状監査用であり、self-host 完了宣言ではない。現時点の stage chain は
次段 artifact を生成するが、stage2 はまだ Kizu の parse / resolve / check / lower / emit
pipeline ではなく、Go が生成した source-scanning LLVM template に依存している。
parser metric は parser-local な部分文字列 scan ではなく、token / illegal-token count で
selfhost lexer の `TokenScan` cursor を使う。declaration keyword count は fixed-point
template と揃えるため、まだ lexer-local の identifier byte matcher に残している。checker は
source summary の brace balance と illegal-token count を受け取り、illegal token がある
stage input を invalid として扱う。stage1 と stage2 以降の metric は `selfhost/kizu.toml` と
`selfhost/src/*.kizu` の両方を stage input として読む。Go LLVM backend は selfhost summary
struct の cross-function return / call / field read を first-class aggregate として emit
できる。

実行した command:

```sh
go run ./cmd/kizu check selfhost
go test ./internal/transpile -count=1
go test ./cmd/kizu -run TestSelfhostStage1ReadsSourceTree -count=1
go test ./...
pre-commit run --all-files

go run ./cmd/kizu build --target native --libc on --runtime hosted --emit exe \
  -o target/selfhost/kizu-stage1 selfhost
target/selfhost/kizu-stage1 target/selfhost/stage2.ll
clang target/selfhost/stage2.ll -o target/selfhost/kizu-stage2
target/selfhost/kizu-stage2 target/selfhost/stage3.ll
clang target/selfhost/stage3.ll -o target/selfhost/kizu-stage3
target/selfhost/kizu-stage3 target/selfhost/stage4.ll
```

artifact path:

```text
stage1 binary: target/selfhost/kizu-stage1
stage2 binary: target/selfhost/kizu-stage2
stage3 binary: target/selfhost/kizu-stage3
stage2 LLVM:   target/selfhost/stage2.ll
stage3 LLVM:   target/selfhost/stage3.ll
stage4 LLVM:   target/selfhost/stage4.ll
```

compile log summary:

```text
stage1 build: target/selfhost/kizu-stage1
stage2 link: clang warning only: overriding the module target triple
stage3 link: clang warning only: overriding the module target triple
```

artifact comparison:

```text
stage2_vs_stage3_bytes=0
stage3_vs_stage4_bytes=0

1099696 target/selfhost/stage2.ll
1099696 target/selfhost/stage3.ll
1099696 target/selfhost/stage4.ll
3299088 total
```

stage2 source metric header:

```text
; kizu stage source metric 1205
; kizu stage source bytes 1141523
; kizu stage source fn count 76
```

remaining Go / template dependency:

- `internal/transpile/gokizu.go` still generates Kizu compiler source and the fixed-point
  source-scanning LLVM artifact.
- `selfhost/src/emit.kizu` still appends a generated LLVM template rather than lowering a real IR.
- stage2 reads `selfhost/kizu.toml` and the selfhost source tree and scans them, but it does not
  execute the Kizu parser, resolver, checker, lowering, and LLVM emitter as a compiler pipeline.
- `selfhost/src/compiler.kizu` uses `SourceMetrics`, `parser::Module`,
  `checker::CheckedModule`, and `lower::Module` summaries. Parser summaries carry first-token,
  declaration, token, illegal-token, byte, function/import/struct/enum, brace, and balance
  metrics, but still not AST, typed IR, or lowered LLVM instructions.
- `selfhost/src/lexer.kizu` now has `TokenScan`, `Scan`, and `Advance` cursor helpers. Token and
  illegal-token metrics use the cursor scanner, but declaration keyword metrics still use a
  byte-level identifier matcher until token-kind keyword counting is stable in native output.
- Go native backend and hosted runtime still build `target/selfhost/kizu-stage1`.
- The generated Kizu parser/checker/lower are partial surfaces, not production replacements for
  `internal/parser`, `internal/types`, or backend lowering.

## Issue 化する作業

- self-host readiness gate を tracking issue にする: #196
- `kizu test` の package/component test 仕様を固める: #197
- cross-module 型参照と imported type usage を conformance 化する: #198
- token/lexer port の stdlib dependency gate を固める: #199
- token/lexer port の blocker を受け入れ条件に反映する: #192
- AST/parser port の blocker を受け入れ条件に反映する: #193
