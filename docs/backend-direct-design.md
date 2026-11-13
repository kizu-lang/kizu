# 自前 native backend(direct)の本流採用(設計)

Kizu IR から machine code を直接エンコードし、object file(Mach-O / ELF / COFF)を
compiler 自身が書く native backend(以下 direct)を main に入れる範囲と順序を記録する
文書です。§8 の判断(D1〜D10)は決定済みです。決まった部分は実装 PR で `SPEC.md` /
`docs/architecture.md` へ移し(ADR は書かない)、この文書は役目を終えたら消します。

direct は main とは別の統合ブランチで実装と検証を済ませています(main 取り込み済み、
main に対して 89 commit 先行、compiler/ を中心に +24,863 / −268)。この PR は docs だけで、
compiler/・CI・SPEC は変えません。それらの変更は実装 PR 1 本(§7)の範囲です。

## 0. 要約

| 項目 | 決定 |
| --- | --- |
| 対象 target | darwin-arm64、linux-amd64、windows-amd64(実機で実行検証のある 3 本) |
| 対象外 | linux-arm64(`--backend direct` では受けるが対象外。生成系は darwin-arm64 と共用)、darwin-amd64(保留、direct を作らない)、windows-arm64(保留) |
| `kizu build` / `kizu run` / `kizu test` | 対象 target では既定を direct(初回から S2)。`--backend llvm` で LLVM。direct の無い target では LLVM |
| `--backend asm` | 全 triple で明示エラー(assembly text の経路は削除済み) |
| bootstrap | LLVM のまま(S3 は今回の範囲外) |
| CI | `difftest-direct` job(ubuntu-latest のみ)を test job と並列に追加。必須チェックにしない。arm・windows のランナーは使わない |
| SPEC | §5 を書き換え、NaN ビット列を実装定義と書く。ADR は書かない |
| 実装 PR | 分割せず 1 本 |
| 品質の担保 | 差分検証・バイト比較・ベンチ・bootstrap の数値(§5) |

## 1. 構成

```text
Kizu IR (--opt なら ir::optimize 済み)
  -> 命令選択(arm64 / amd64)-> 機械 IR(仮想レジスタ)
  -> MIR 最適化・ループのベクトル化・線形スキャン RA
  -> エンコーダ(arm64 / x86-64)-> 共有 object バッファ
  -> 書き出し層: Mach-O | ELF | COFF
  -> host の linker(clang ドライバ経由)で runtime C と link
```

- 置き場所は `compiler/src/internal/arm64/`(約 23,800 行、amd64 を含む)。
  名前は歴史的経緯で、x86-64 も同じ module にある
- 命令の byte 列は arch ごとに 1 つのエンコーダが書き、形式で変わるのは書き出し層だけ
- 未対応の形は `arm64 error:` / `amd64 error:` で止まる。LLVM への暗黙の切り替えはしない
- assembly text の印字層は削除済み。direct は `.s` を経由しない
- ループのベクトル化は IR 命令と CFG の形を認識して行う。原理 11 が禁じるのはソースの形
  ごとの payload 型・関数名分岐・形状 template で、IR の形の認識は原理 11 の対象外とする
  (D4)。関数名やソースの書き方で分岐する認識は入れない

## 2. 対象 target

| target | 状態 | 書き出し |
| --- | --- | --- |
| darwin-arm64 | 対象 | Mach-O |
| linux-amd64 | 対象 | ELF x86-64 |
| windows-amd64 | 対象 | COFF |
| linux-arm64 | 対象外。`--backend direct` では受ける。実装と検証(difftest 261/0/0、`clang -c` とのバイト一致)はある | ELF AArch64(ELF 層は linux-amd64 と共用) |
| darwin-amd64 | 保留。direct を作らない。LLVM | ― |
| windows-arm64 | 保留 | ― |

linux-arm64 は `--backend direct` で受けたまま、対象外と明記する(D2)。命令選択・
エンコーダは darwin-arm64 と、ELF 層は linux-amd64 と共用なので、生成系の検証は
その 2 target で続く。既定は LLVM のままで、CI でも常時は回さない。linux-arm64 が
必要になったら、Mac 上の一時的な VM / コンテナで実装・検証する(常駐の環境は置かない)。
その時期と、既定を direct にするかは今回の範囲外で後日判断する。

## 3. CLI

| 入力 | 振る舞い |
| --- | --- |
| `kizu build` / `kizu run` / `kizu test`(フラグなし) | 対象 target なら direct、他は LLVM |
| `--backend direct` | 対象 target と linux-arm64 なら direct。他は `error: --backend direct needs ... target` |
| `--backend llvm` | LLVM(build / run / test とも受ける) |
| `--backend asm`(全 triple) | exit 1、出力なし、`error: --backend asm (assembly text) was removed; ...` |

- `--backend` は `kizu help`(usage)に載せる(D10)。今も指定はでき、載っていないのは
  usage 表示だけ
- backend は target で決まり、同じ target では常に同じ backend になる。direct の無い
  target で LLVM を使うのは fallback ではなく target ごとの選択(wasm が別の生成系を
  持つのと同じ)。direct が未対応の形で止まっても LLVM へは切り替えない(原理 9)
- 実装 PR で SPEC §5 の「native code は LLVM IR backend が生成します」を、backend は
  target ごとに決まる(対象 target は direct、他は LLVM)という記述に書き換える(D3)。
  ADR は書かない

## 4. CI

実装 PR で入れる形:

- job `difftest-direct`、`ubuntu-latest` のみ、timeout 10 分。統合ブランチにある
  `ubuntu-24.04-arm` の matrix は消す(D2)
- `scripts/bootstrap.sh --stage1-only` → `scripts/difftest-direct.sh target/seed/stage1/bin/kizu`
- `scripts/difftest-direct.sh` は `examples/*.kizu` の `run` / `run-fails` 261 件を
  direct と LLVM で build・実行し、stdout・stderr・exit status の全文一致を求める
  (既定が direct になるので、両側とも `--backend` を明示する)
- 既存 `test` job は変えない。既定が direct なので、macOS / Linux の test job は
  compiler 自身の test と test 系 case を direct で回す(Mach-O 側はここで担保)
- debug のみ、cache なし。macOS は 1 件ごとの link と初回起動が遅く(ローカルで 10〜13 分)外した
- `difftest-direct` は必須チェックにしない。job としては回す(D5)。CI はミニマムに保ち、
  開発効率を落とさない
- windows の job は足さない。windows-amd64 の検証はローカル実機に頼る(D6)

| 項目 | 見積もり(ローカル実測から) |
| --- | --- |
| 新 job 1 本 | 3〜4 分(ローカルの WSL で 115 s) |
| merge までの wall 時間 | +約 0 分(既存 test job 9〜11 分と並列) |
| ランナー消費 | 1 push あたり +約 3〜4 分(arm を消して job 1 本) |

実ランナーでの所要時間は未計測で、実装 PR の run で確認する。

## 5. 検証の証拠

数値の出典は統合ブランチの検証記録で、括弧内がその記録名。
ベンチは `--opt` 同士、direct / LLVM の実行時間比、交互 31 回の中央値。1.00 未満は direct が速い。

### 5.1 最新の統合状態(fa8ec0c1 = main c9cfa9f8 取り込み後、記録「統合 run2+main」)

| 項目 | 結果 |
| --- | --- |
| difftest(`run` 261 件、一致 / 不一致 / 未対応) | 3 target とも debug・`--opt` で **261 / 0 / 0**(windows debug は生成物ファイル消失型の環境揺れ 4 件を単独再実行で一致) |
| 生成物バイト比較(main 取り込み前後) | 3 target とも 554 / 554 同一 |
| self-build(direct、debug / `--opt`) | 3 target exit 0、hello 実行可 |
| ベンチ 11 本 幾何平均(2 回) | darwin-arm64 **0.947 / 0.945**、linux-amd64 **0.921 / 0.923**、windows-amd64 **0.937 / 0.940** |
| f64 カーネル 4 本(2 回) | darwin 1.003 / 1.006、linux-amd64 1.008 / 1.012、windows-amd64 **1.012 / 1.005** |
| `just selfhost`(種 v0.6.0) | 通過。behavior 15 cases 0 failed、`stage2 reproduces stage1 (12556184 bytes)` |

### 5.2 エンコーダと書き出し層(バイト比較)

direct の object と、同じ機械 IR を assembly text にして `clang -c` したものをファイル全体で比較。

| target | examples 261(debug / `--opt`) | 追加ケース | 記録 |
| --- | --- | --- | --- |
| darwin-arm64(Mach-O) | 261/261 ×2 | 52/52 ×2、実行ファイルもバイト同一 | E1 |
| linux-arm64(ELF) | 261/261 ×2 | 52/52 ×2 | E2 |
| linux-amd64(ELF x86-64) | 261/261 ×2 | 64/64 ×2 | E3 |
| windows-amd64(COFF) | 261/261 ×2 | 64/64 ×2、compiler 自身、再配置数 65532〜70000 の境界 4/4 | E4 |

実行ファイル比較: linux は入力ファイル名の FILE シンボル、windows は lld のリンク時刻 4 byte ×2 を除いて同一。

### 5.3 その他

| 項目 | 結果 | 記録 |
| --- | --- | --- |
| build `--opt` 時間(darwin、direct / LLVM) | fib 60.3 / 77.4 ms、crypto_ecdsa 75.4 / 201.1 ms、http_serve 151.3 / 1475.6 ms | E1 |
| build `--opt` 時間(direct / assembly text 経路) | darwin 0.85〜0.35、linux-amd64 0.87〜0.79、windows-amd64 0.83〜0.78(大きいプログラムほど効く) | E1・E3・E4 |
| windows f64 の改善 | 7.621 / 7.746 → 1.012 / 1.010(xmm6..15 の callee-saved 対応) | WF64 |
| windows 浮動小数境界テスト 12 ファイル | 丸め・NaN の payload / 符号・±0 を含めて変更前とバイト一致 | WF64 |
| `kizu test` 既定 direct | 3 target でテスト資産 11/11 が LLVM と全文一致。`--backend asm` は build 30/30・test 15/15 が removed エラー | 統合 tdef2 |
| CI job のローカル再現 | linux-arm64・linux-amd64・darwin-arm64 で 261/0。否定テストで差分・build 失敗を exit 1 で検出 | CI1 |
| ベクトル化の境界テスト | 24 本 × debug / `--opt` で LLVM と一致 | Phase 2 |

## 6. 既知の制限

- darwin-amd64: direct なし。LLVM。実機での実行検証なし
- linux-arm64: 対象外(実装は残り、`--backend direct` で受ける。§2)
- windows-arm64: 保留。実機での実行検証なし
- NaN のビット列(payload・符号)は実装定義。LLVM `--opt` も演算のオペランドを入れ替えて
  NaN の選び方を変えるため、direct と LLVM でビット列が違い得る。実装 PR で SPEC に
  「NaN のビット列は実装定義」と書く(D8)。SPEC が規定するのは IEEE 754 と等値比較の規則
- 性能の残差(amd64): windows bits 1.09〜1.12、gcd 1.05〜1.07、sieve 1.04〜1.11、
  fmandel 1.04〜1.06(全 target)。`2.0*x` → `x+x` は意味論に触れるので入れていない
- windows の SEH unwind 情報(`.pdata` / `.xdata`)は出さない
- 分岐の到達範囲超過(arm64 ±1MB / ±128MB)は診断で止まる(examples では起きない)
- `--opt` の difftest、windows・linux-arm64 の difftest は CI に無い(ローカルで回す)

## 7. 実装 PR(1 本)

実装 PR は分割せず 1 本で出す(D9)。統合ブランチで検証済みの状態をそのまま入れる。
内訳(レビューで追うときの技術的境界):

| 内訳 | 内容 |
| --- | --- |
| arm64 直接エンコード基盤 | 機械 IR・命令選択・RA・ベクトル化・arm64 エンコーダ・共有 object バッファ・Mach-O |
| ELF 層 + x86-64 エンコーダ | amd64 選択・エンコーダ・ELF(x86-64 / AArch64) |
| COFF 層 | windows-amd64。Win64 の xmm 退避を含む |
| CLI と既定化(S2) | build / run / test の既定 direct、`--backend llvm` の opt-out、`--backend asm` の removed エラー、`--backend` の usage 掲載 |
| bootstrap | `scripts/bootstrap.sh` の stage2 build に `--backend llvm` を明示し、bootstrap を LLVM のまま保つ(S3 は範囲外) |
| CI | `difftest-direct` job(ubuntu-latest のみ)と script |
| 文書 | SPEC §5 の書き換え、NaN ビット列の実装定義、`docs/architecture.md`。この設計文書の削除 |

- assembly text の経路は main に入れない(統合ブランチでは実装後に削除済み。エンコーダの
  正しさは削除前に取ったバイト比較が担保する)
- 完了条件は §8 D7 の S2 の証拠を実装 PR に揃えること

## 8. 決定(D1〜D10)

| # | 問い | 決定 |
| --- | --- | --- |
| D1 | direct を main に入れるか | 入れる |
| D2 | linux-arm64 の扱い | CI の arm ランナー(`ubuntu-24.04-arm`)は消す。`--backend direct` では受けたまま対象外と明記する(生成系は darwin-arm64 と共用で検証が続く)。必要になったら Mac 上の一時的な VM / コンテナで実装・検証する(常駐させない) |
| D3 | 原理 9・SPEC §5 との関係 | SPEC §5 を書き換え、backend は target ごとの選択と書く。ADR は書かない |
| D4 | 原理 11 との関係 | ループのベクトル化(IR 命令と CFG の形の認識)は原理 11 の下で許す。IR の形の認識は原理 11 の対象外とする |
| D5 | CI の必須チェック | `difftest-direct` は必須チェックにしない(job としては回す)。CI はミニマムに保ち開発効率を落とさない |
| D6 | windows の CI | job を足さない。ローカル実機の検証に頼る |
| D7 | 既定化の段階 | 段階を踏まず初回から S2 まで入れる(build / run / test を対象 target で direct 既定)。完了条件は下表 |
| D8 | NaN ビット列 | 「実装定義」と SPEC に書く |
| D9 | 分割 | 実装 PR は分割せず 1 本 |
| D10 | `--backend` の usage 掲載 | `kizu help`(usage)に載せる |

実装 PR に揃える証拠(D7、S2 の完了条件):

- 3 target の difftest 261/0/0(debug・`--opt`)
- テスト資産の LLVM との一致、`just selfhost` 通過
- ベンチ 11 本の幾何平均が 3 target とも < 1.00、f64 4 本が < 1.05
- windows-amd64 は実機の記録(D6)
- 実装 PR の run で `difftest-direct` が緑(必須チェックではない)

今回の範囲外(後日判断):

- S3: bootstrap(stage1 → stage2)の direct 化。種が direct を知るリリースへの更新
  (`docs/bootstrap.md` の 2 リリース規律)が前提
- S4: LLVM を対象 target から外すか。外さないなら比較基準と非対象 target のために残る
- linux-arm64 の対応時期と既定化
