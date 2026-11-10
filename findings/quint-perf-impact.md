# Quint 検証の追加が性能に与えた影響

## 結論

**無視できる範囲。** コンパイラの実行経路と生成コードには影響なし。テスト時間は
増えたが、増えた分はクリティカルパス外のパッケージにあり、壁時計への寄与は小さい。

| 対象 | 判定 | 要点 |
| --- | --- | --- |
| `kizu build` / `check` / `run` | 影響なし | Quint 成果物はどの実行経路にも入っていない。キャンペーンで入ったコンパイラ側の変更は 2 件のバグ修正だけで、`check compiler` は前後で同じ時間 |
| `go test ./cmd/kizu` | 影響なし | Quint 由来のテストを 1 つも含まない。約 100s の内訳は selfhost / conformance 系 |
| `go test ./...`(手元) | 小さい(約 +3.4s, 3.5%) | MBT は直列換算で約 11.5s 増えたが、他パッケージと並行で走るため壁時計は CPU 競合分だけ伸びる |
| CI | 判別できない | 壁時計は `cmd/kizu` が律速。ランナーのばらつき(±50%)の方が桁違いに大きい |

## 測定条件

- マシン: Apple M4 Max(16 コア, 128 GB), macOS 27.0, Go 1.23.4, node v26.13.0
- 基準: 現 main = `7a06a6c`。キャンペーン前 = `24b0b09`(#1921 のマージ直前)
- `go test -count=1 -json` の package / test の `Elapsed` を読む。各条件の前に 1 回
  build だけ走らせてキャッシュを温める。回数と中央値は各表に書く
- MBT テストの識別: テスト名が `Model|Divergence` に一致するもの。リポジトリ内で
  一致するのは `internal/{ownership,ir,unsafecap,cimport}` の `*mbt_test.go` の
  17 本だけ(`git grep 'func Test[A-Za-z]*(Model|Divergence)'` で確認)

## 1. テスト時間

### PR ごとの増分(対象パッケージ単体, 各 5 回の中央値)

マージコミットの第 1 親(直前)とマージコミットで、そのパッケージだけを走らせた。

| PR | パッケージ | 直前 | マージ後 | 増分 | うち MBT テスト |
| --- | --- | ---: | ---: | ---: | ---: |
| #1921 ownership | ownership | 5.85s | 8.62s | +2.77s | 2.78s |
| #1926 unsafecap | unsafecap | 0.00s | 1.50s | +1.50s | 1.43s |
| #1922 ir-safety | ir | 7.68s | 10.53s | +2.85s | 2.89s |
| #1931 cimport | cimport | 0.06s | 1.59s | +1.53s | 1.51s |
| #1948 unsafecap F2/F3 | unsafecap | 1.49s | 1.49s | ±0 | 1.40→1.41s |
| #1949 ir mutants | ir | 10.81s | 11.27s | (誤差) | 3.00→2.96s |
| #1953 ownership 層化 | ownership | 8.44s | 11.27s | +2.83s | 2.77→5.60s |
| **合計** | | | | **約 +11.5s** | |

各範囲(min–max)は ±0.1s 程度に収まる(#1949 のみ 10.3–12.5s と揺れたが MBT 部分は不変)。
#1953 の「replay 約 2.6s → 約 5.4s」は再現した(`TestOwnershipModelTraces` 2.65s → 5.46s)。

テスト別(現 main, 単体実行): `TestOwnershipModelTraces` 5.6s, `TestSlotModelTraces` 1.7s,
`TestCImportModelTraces` 1.4s, `TestUnsafeModelTraces` 1.2s, `TestBoundsModelTraces` 0.9s,
その他は各 0.2s 以下。

### 全体への寄与(現 main, `go test ./...`, MBT あり/なしを交互に各 3 回)

| 条件 | 壁時計(各回) | 中央値 |
| --- | --- | ---: |
| そのまま | 99.5 / 95.2 / 97.0s | **97.0s** |
| `-skip 'Model\|Divergence'` | 95.3 / 93.1 / 93.7s | **93.7s** |

差は約 3.4s。律速は `cmd/kizu`(93–99s)と `internal/selfhost`(92–96s)で、MBT を含む
4 パッケージはそれより先に終わる。MBT は全体実行中には競合で単体より遅く(合計 13–18s)、
その CPU 消費が `cmd/kizu` を 2–4s 押し延ばしている、というのが 3.4s の中身。

### `go test ./cmd/kizu`(キャンペーン前後, 交互に各 5 回)

1 回目は Kizu の build cache が commit ごとに冷えるため外れ値(221s / 179s)。2–5 回目:

| commit | 各回 | 中央値 |
| --- | --- | ---: |
| `24b0b09`(前) | 105.5 / 108.3 / 105.7 / 104.0s | 105.6s |
| `7a06a6c`(現) | 105.4 / 96.3 / 102.9 / 102.8s | 102.8s |

増えていない。キャンペーンの PR は `cmd/kizu` に 1 ファイルも触れておらず
(期間中に `cmd/kizu` を変えたのは #1928 だけ)、Quint 由来のテストも含まない。
支配的なのは `TestConformance`(16–30s), `TestSelfhostFrontend`(19–28s),
`TestSelfhostBootstrap`(15s), `TestSelfhostBehavior`(12–18s),
`TestEveryExampleLowersToTheSameVerifiedIR`(12s)で、この 5 本で約 8 割。
以前記録された「約 90〜99s」はこの構成によるもので、Quint 起因ではない。
(`cmd/kizu` に Quint 関係で唯一あるのは #1910 由来の `TestTlsSpecTablesComeFromStdTls`
で 0.1s。)

## 2. CI 時間

`ci.yml` の Test step、main への push 37 run(`bf147f4`〜`7a06a6c`)の job log から
パッケージ別時間を抽出した。

- Test step の壁時計は macOS 164–279s, ubuntu 129–258s と、同じ内容の連続 commit
  でも ±50% 振れる。壁時計は `cmd/kizu` の時間 +数秒で、MBT パッケージは律速にならない。
- ランナー速度の差を消すため、キャンペーンで変わっていない `internal/llvm` との比で見た
  (各期間の中央値)。

| 期間 | OS | ownership/llvm | ir/llvm | cmd/kizu/llvm |
| --- | --- | ---: | ---: | ---: |
| 前(6 run) | macOS | 0.42 | 0.57 | 5.27 |
| #1921〜#1931(7) | macOS | 0.61 | 0.81 | 5.42 |
| 〜#1952(18) | macOS | 0.61 | 0.84 | 5.40 |
| #1953 以降(6) | macOS | 0.75 | 0.82 | 4.98 |
| 前(6 run) | ubuntu | 0.47 | 0.64 | 3.79 |
| #1921〜#1931(7) | ubuntu | 0.67 | 0.80 | 3.61 |
| 〜#1952(18) | ubuntu | 0.64 | 0.85 | 3.29 |
| #1953 以降(6) | ubuntu | 0.88 | 0.84 | 3.35 |

ownership・ir の相対時間は手元の測定どおり段階的に上がった。律速の `cmd/kizu` の比は
上がっていないので、CI の所要時間への影響はばらつきに埋もれて検出できない。
ubuntu の ownership は現在 25–64s で、`cmd/kizu`(125–252s)の 1/4 程度。

## 3. コンパイラ本体と生成コード

確認方法(`7a06a6c` に対して):

- `git grep -E 'spec/|[Qq]uint|_mbt|mbt\.json' -- '*.go' ':!*_test.go' ':!scripts/'`
  → `internal/machinequint` 自身以外に一致なし。
  `internal/machinequint` を import するのは `cmd/kizu/machine_spec_test.go` と
  `scripts/machine-quint` だけ。
- `compiler/` と `lib/` に `spec/` の Quint モデルや `mbt` への参照なし。
- 非テストの `go:embed` は `internal/native/runtime/runtime.c` の 1 件だけ。
- `go list -deps ./cmd/kizu` に `machinequint` も `scripts/` も現れない。
- fixture(`internal/*/testdata/*mbt*`, 計 1.4 MB)は `_test.go` からしか読まれない。
  `scripts/*-mbt` は `just spec-*` から走る別の `package main`。
- キャンペーン PR(#1921 #1922 #1926 #1931 #1948 #1949 #1953)の差分で、テスト・
  `spec/`・`findings/`・`testdata`・`scripts/` 以外に触れたのは `justfile` と
  `ci.yml`(node 20 の pin)だけ。

コンパイラ側の検証パスは**キャンペーンで追加されていない**。常時オンの IR verifier
(`internal/ir/verify.go`)はキャンペーン以前からあり、期間中は変更なし。キャンペーンが
起点になったコンパイラ変更は次の 2 件のバグ修正だけ:

| PR | 変更 | 性能への影響 |
| --- | --- | --- |
| #1924 | `applyBoundsPlan` が既存の `%bce<n>` の最大番号を数えてから採番(`lastBoundsSite`) | `--opt` の bounds pass 1 回あたり関数の命令を 1 回走査するだけ。pass 本体と同じ線形 |
| #1930 | `unsafe struct` のフィールドを `&var` で貸す箇所に unsafe を要求 | 型検査の分岐追加。seed の `kizu check compiler` を前後(`791552c` / `12cf3c3`)で各 5 回: real 0.87s / 0.88s, user 1.45s / 1.46s(中央値)。差なし |

## 緩和案(実施はしていない)

現状は対処不要。ただし ownership MBT は層化のたびに倍になる傾向があるので、
次の候補を用意しておく。

1. **既定は層ごとの代表だけ、全量は `just spec-ownership` 側で再生する。**
   既定の `go test` から外れる分だけ確実に減る。ownership で約 5s。
2. **`TestOwnershipModelTraces` の case を `t.Parallel()` のサブテストにする。**
   単体時間は縮むが、全体実行では他パッケージと CPU を奪い合うので、壁時計への効果は
   1. より小さい。
3. **MBT を別の CI job に分ける。** CI の壁時計は今でも `cmd/kizu` 律速なので、
   CI については効果がほぼない。pre-push(手元の `go test ./...`)の 3.4s を
   消したいときだけ意味がある。

判断の目安: MBT を含むパッケージの単体時間が `cmd/kizu` / `internal/selfhost` の律速時間に
近づくまでは、壁時計への寄与は CPU 競合分(数 %)にとどまる。
