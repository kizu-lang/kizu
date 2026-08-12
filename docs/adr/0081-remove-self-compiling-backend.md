# ADR-0081: 自己コンパイル backend を撤去し、backend は Go 参照実装の IR に合わせて作り直す

Status: 採用

Supersedes: ADR-0080 の「backend は一般化で育てて shapes を退役させる」運用方針

## 背景

ADR-0080 は「新しい形状 lowering を追加しない、一般 lowering を拡張して既存 shapes を
退役させる」と決めた。方向は正しかったが、規模を測っていなかった。

2026-08-12 時点の実測:

| 層 | Go 参照実装 | selfhost | 比 |
| --- | ---: | ---: | ---: |
| 全体(test 除く) | 42,738 | 164,011 | 3.8x |
| IR(`internal/ir` ↔ `selfhost/src/ir`) | 2,730 | 43,669 | 16x |
| backend(`internal/llvm` ↔ `selfhost/src/backend`) | 3,426 | 76,977 | 22x |
| うち自己コンパイル専用(`compiled_*`) | — | 63,590 | — |
| 手書き LLVM 文字列リテラル | 0 行 | 2,367 行 | — |

原因は表現の選び方にある。Go の IR は op を 17 種持つ**汎用 `Instr` 1 個**である
(`internal/ir/ir.go`)。対して selfhost の MIR は **58 個の専用 payload 型 /
62 union variant / 248 個の専用 renderer** を持ち、ソースの形ごとに型を作っていた。

この構造では、新しい書き方が来るたびに payload 型・lowering・renderer の 3 点セットが
増える。各形状は自分の知る形しか描画できないので、ソースが育つと黙って壊れる。
PR #1492 がその実例で、`is_type_apply_start` の専用 template が成長したソースの意味論を
落とし、hosted stage1/stage2 のパーサが generic 呼び出しを全滅させていた。

そして 1 件あたり約 95 行の退役を 540 回繰り返す計画は、追加の速度に負ける。
漸進退役は算術的に成立しない。

## 決定

### 1. `compiled_*` 系の自己コンパイル backend を削除する

`selfhost/src/backend/` の `compiled_*` / `cli_*_llvm` / `llvm.kizu` と、それを支える
fact 契約・MIR 環境・gate を削除する。あわせて `stage` サブコマンドと bootstrap
(stage0→stage1→stage2)を廃止する。

削除実績: selfhost 側 76,608 行(164,011 → 87,403)。

### 2. selfhost 実行ファイルを作るのは stage0(Go backend)だけとする

ADR-0080 が「必須要件」としていた経路をそのまま唯一の経路にする。

```text
selfhost/src/*.kizu  --(Go backend: kizu build --target native)-->  実行ファイル
```

すべての parity gate はこの実行ファイルに対して回す。

### 3. 自己コンパイルは、backend を作り直したあとに再導入する

新 backend は Go の `internal/ir` + `internal/llvm`(合計 6,156 行、動作する参照実装)
の構造に合わせる。すなわち:

- MIR は「op を持つ汎用命令」1 種にする。ソースの形ごとの payload 型を作らない。
- lowering は AST を再帰的に歩いて命令列を作る。形状判定をしない。
- renderer は「命令 → LLVM 行」の 1:1 にする。関数名で分岐しない。
- LLVM を文字列リテラルで書かない(ADR-0073 の再確認)。

## 影響

- `kizu stage` は無くなる。selfhost の CLI は `check` / `parse` / `run` / `test` / `fmt`。
- nightly CI は bootstrap 比較ではなく「stage0-native ビルド + parity gate」を回す。
- `docs/selfhost-backend-generalization.md`(shapes 退役台帳)は役目を終える。
  退役対象そのものが消えたため、台帳は履歴として残し、新規記入はしない。
- 自己コンパイルは flip-readiness gate ですらなくなり、**将来の作業項目**になる。

## この削除が明らかにしたこと

stage2 の挙動が selfhost ソースの挙動と一致していなかった箇所が、削除によって初めて
可視化された。いずれも「手書き LLVM がソースと別の意味論を持っていた」ことによる。

- `parse` の golden 14 件は、ソースを整形せずそのまま echo する stage2 の挙動を固定して
  いた。ソース実装は AST を再描画する。golden を実装から再生成した。
- `run` の unsupported 経路は、`code_render` が error を返すため `execute.kizu` の
  `unsupported_run_codegen` 分岐が到達不能だった。空 module を返す設計どおりに戻した。
- `test` parity gate は「compiler 呼び出しは無出力」を前提にしていた。`kizu test` は
  テストを実行するので、compiler の出力がテスト結果そのものである。
- selfhost checker は `for 0..2 |n|` の束縛が同名の関数スコープ local と衝突すると
  (宣言順を問わず)診断を出す。`if` block 内の `let` shadowing は通る。for 束縛に
  自分のスコープが与えられていない。`run_shadowing` の parity 行を park し、gap として
  記録した。
