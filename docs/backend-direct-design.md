# 自前 native backend(direct)の本流採用(設計)

Kizu IR から machine code を直接エンコードし、object file(Mach-O / ELF / COFF)を
compiler 自身が書く native backend(以下 direct)を main に入れるかと、入れるなら
どの範囲・どの順で入れるかを決める文書です。決まった部分は実装と同時に `SPEC.md` /
`docs/architecture.md` / 該当 ADR へ移し、この文書は役目を終えたら消します。

direct は main とは別の統合ブランチで実装と検証を済ませています(main 取り込み済み、
main に対して 89 commit 先行、compiler/ を中心に +24,863 / −268)。この PR は docs だけで、
compiler/ は変えません。§8 の「決めること」が決まってから実装 PR を出します。

## 0. 要約

| 項目 | 案 |
| --- | --- |
| 対象 target | darwin-arm64、linux-amd64、windows-amd64(実機で実行検証のある 3 本) |
| 対象外 | linux-arm64(ELF 書き出しは linux-amd64 と共用で残る)、darwin-amd64(保留、direct を作らない)、windows-arm64(保留) |
| `kizu build` / `kizu run` | 既定は LLVM のまま。`--backend direct` で direct |
| `kizu test` | 既定を direct。`--backend llvm` で opt-out。direct の無い target では LLVM |
| `--backend asm` | 全 triple で明示エラー(assembly text の経路は削除済み) |
| CI | `difftest-direct` job を test job と並列に追加(wall 時間の増加 約 0 分) |
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

## 2. 対象 target

| target | 状態 | 書き出し |
| --- | --- | --- |
| darwin-arm64 | 対象 | Mach-O |
| linux-amd64 | 対象 | ELF x86-64 |
| windows-amd64 | 対象 | COFF |
| linux-arm64 | 対象外。実装と検証(difftest 261/0/0、`clang -c` とのバイト一致)はある | ELF AArch64(ELF 層は linux-amd64 と共用) |
| darwin-amd64 | 保留。direct を作らない。`kizu test` は LLVM | ― |
| windows-arm64 | 保留 | ― |

linux-arm64 を対象外にすると、`--backend direct` がそれを受けるか拒否するかと、
CI1 の `ubuntu-24.04-arm` ランナーを残すかが決まっていない(§8 D2)。

## 3. CLI

| 入力 | 振る舞い |
| --- | --- |
| `kizu build` / `kizu run`(フラグなし) | LLVM |
| `kizu build --backend direct` | 対象 target なら direct。他は `error: --backend direct needs ... target` |
| `kizu test`(フラグなし) | 対象 target なら direct、他は LLVM |
| `kizu test --backend llvm` | LLVM(`llvm` を受けるのは `test` だけ) |
| `--backend asm`(build / test、全 triple) | exit 1、出力なし、`error: --backend asm (assembly text) was removed; ...` |

`kizu test` の LLVM への切り替えは target で決まり、同じ target では常に同じ backend に
なる。ただし原理 9(経路は 1 本、hidden fallback を入れない)と SPEC §5 の
「native code は LLVM IR backend が生成します」とは文言上ぶつかる(§8 D3)。

## 4. CI

統合ブランチに入っている形:

- job `difftest-direct`、matrix `[ubuntu-latest, ubuntu-24.04-arm]`、timeout 10 分
- `scripts/bootstrap.sh --stage1-only` → `scripts/difftest-direct.sh target/seed/stage1/bin/kizu`
- `scripts/difftest-direct.sh` は `examples/*.kizu` の `run` / `run-fails` 261 件を
  direct と LLVM で build・実行し、stdout・stderr・exit status の全文一致を求める
- 既存 `test` job は変えない。`kizu test` が既定 direct なので、macOS / Linux の test job は
  compiler 自身の test と test 系 case を direct で回す(Mach-O 側はここで担保)
- debug のみ、cache なし。macOS は 1 件ごとの link と初回起動が遅く(ローカルで 10〜13 分)外した

| 項目 | 見積もり(ローカル実測から) |
| --- | --- |
| 新 job 1 本 | 3〜4 分(linux-arm64 コンテナ 201 s、WSL 115 s) |
| merge までの wall 時間 | +約 0 分(既存 test job 9〜11 分と並列) |
| ランナー消費 | 1 push あたり +約 6〜8 分 |

実ランナーでの所要時間は未計測で、最初の PR の run で確認する。
**必須チェックにするかはリポジトリ設定側の判断**で、docs は「必須」と書かない(§8 D5)。
windows-amd64 は CI に job 自体が無く、direct の検証はローカル実機に頼る(§8 D6)。

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

- darwin-amd64: direct なし。`kizu test` は LLVM。実機での実行検証なし
- linux-arm64: 対象外(実装は残る。§8 D2)
- windows-arm64: 保留。実機での実行検証なし
- NaN のビット列(payload・符号)は規定しない(実装定義)。LLVM `--opt` も演算の
  オペランドを入れ替えて NaN の選び方を変えるため、direct と LLVM でビット列が違い得る。
  SPEC は IEEE 754 と等値比較の規則だけを持つ
- 性能の残差(amd64): windows bits 1.09〜1.12、gcd 1.05〜1.07、sieve 1.04〜1.11、
  fmandel 1.04〜1.06(全 target)。`2.0*x` → `x+x` は意味論に触れるので入れていない
- windows の SEH unwind 情報(`.pdata` / `.xdata`)は出さない
- 分岐の到達範囲超過(arm64 ±1MB / ±128MB)は診断で止まる(examples では起きない)
- `--opt` の difftest と windows の difftest は CI に無い

## 7. 実装 PR の分割案(技術的境界)

レビューの都合ではなく、生成物とゲートが独立に検証できる境界で切る。各 PR の完了条件は
§5 の該当ゲート(difftest 261/0/0、バイト比較、ベンチ、`just selfhost`)。

| 順 | PR | 内容 | 主なゲート |
| --- | --- | --- | --- |
| 1 | arm64 直接エンコード基盤 | 機械 IR・命令選択・RA・ベクトル化・arm64 エンコーダ・共有 object バッファ・Mach-O。`--backend direct`(darwin-arm64) | darwin difftest、Mach-O バイト比較、ベンチ darwin |
| 2 | ELF 層 + x86-64 エンコーダ | amd64 選択・エンコーダ・ELF x86-64。linux-amd64 を受ける | linux-amd64 difftest、ELF バイト比較 |
| 3 | COFF 層 | windows-amd64 を受ける。Win64 の xmm 退避を含む | windows 実機 difftest、COFF バイト比較、f64 ベンチ |
| 4 | `kizu test` 既定化 | `--backend llvm` の opt-out、`--backend asm` の removed エラー | 3 target のテスト資産一致、`just selfhost` |
| 5 | CI | `difftest-direct` job と script | 実ランナーでの所要時間 |

- assembly text の経路は main に入れない(統合ブランチでは実装後に削除済み。エンコーダの
  正しさは削除前に取ったバイト比較が担保する)。入れてから消す順にはしない
- 1 は規模が大きい(`internal/arm64/` は全体で約 23,800 行、その多くが 1)。arch と形式の
  境界以外に技術的に独立な切れ目は「ベクトル化を後から足す」くらいで、ベクトル化前の
  計測では darwin の scan が 2.75 倍だった(Phase 1)
- 2 と 3 は 1 に依存し、互いには独立(COFF は ELF と x86-64 エンコーダを共有するので 2 の後)
- 4・5 は 1 の後ならいつでもよい。5 を先に入れると以後の PR が CI で direct を検証される
- 代案: 統合ブランチを 1 本の PR として入れる(検証済みの状態そのものを入れる)

## 8. 決めること

| # | 問い | 案 |
| --- | --- | --- |
| D1 | direct を main に入れるか | 入れる |
| D2 | linux-arm64 の扱い | (a) `--backend direct` で受けたまま対象外と書く、(b) 拒否する。CI の `ubuntu-24.04-arm` を残すか |
| D3 | 原理 9・SPEC §5 との関係 | backend は target で決まる(wasm と同じ)と SPEC に書き、`kizu test` の LLVM 切り替えを fallback でなく target ごとの選択とする。SPEC の「LLVM IR backend が生成します」を書き換える。ADR が要るか |
| D4 | 原理 11 との関係 | ループのベクトル化は IR 命令と CFG の形を認識する(関数名・ソース形ではない)。これを原理 11 の下で許すか |
| D5 | CI の必須チェック | `difftest-direct (*)` を required checks にするか(リポジトリ設定) |
| D6 | windows の CI | windows ランナーの job を足すか、ローカル実機の検証に頼るか |
| D7 | 既定化の段階と完了条件 | 下表 |
| D8 | NaN ビット列 | 実装定義のまま SPEC に書くか、何も書かないか |
| D9 | 分割 | §7 の 5 PR か、1 PR か |
| D10 | `--backend` の usage 掲載 | `kizu help` に載せるか(現状は載せていない) |

既定化の段階(D7):

| 段 | 既定 | 完了条件(ゲート証拠) |
| --- | --- | --- |
| S1 | `kizu test` だけ direct | 3 target difftest 261/0/0(debug・`--opt`)、テスト資産の一致、`just selfhost` 通過、CI `difftest-direct` 緑 |
| S2 | `kizu build` / `kizu run` も対象 target で direct | S1 + ベンチ 11 本 3 target とも < 1.00、f64 4 本 < 1.05、windows を含む CI か実機記録、一定期間の CI 緑 |
| S3 | bootstrap(stage1 → stage2)も direct | S2 + stage2 の自己再現、種が direct を知るリリースへの更新(`docs/bootstrap.md` の 2 リリース規律) |
| S4 | LLVM を対象 target から外すか | S3 + 判断。外さないなら比較基準と非対象 target のために残る |
