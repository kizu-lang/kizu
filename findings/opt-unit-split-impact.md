# `--opt` に unit 分割を広げた場合のデメリット(調査)

前提は PR #1929 の設計文書 `docs/build-cache-design.md`(この checkout の main には
まだ無く、PR の branch `design/module-build-cache` にある)の §4(A1 では `--opt` は
single unit のまま)、§5(bootstrap の一致規則)、§9 U3(ThinLTO の採否)。
本実装はしていない。数値は使い捨ての実験 build(末尾「実験の作り方」)で測った。

## 結論

**広げることを止める決定的な理由は見つからなかった。ただし「分割のみ」は release 用の
`--opt` としては不可で、広げるなら「分割 + ThinLTO(+ LTO cache)」が条件になる。**

- 分割のみ: build は大きく縮む(compiler 自身の cold 32.8 s → 9.2 s、20k fixture
  14.0 s → 1.0 s)が、実行性能が最大 13% 落ちる。落ちるのは shipping する compiler
  自身(`emit-llvm compiler` 1.11×、`check` 50k 1.13×)で、`--opt` の目的
  (binary の速さ)に反する。
- 分割 + ThinLTO: 実行性能は現状と同等(compiler で 0.97〜1.00×)に戻り、cold build は
  分割のみとほぼ同じ(compiler 10.4 s)。代償は (a) LTO cache が無いと増分でも link で
  backend を全部やり直す(compiler の 1 module 編集 8.0 s、cache ありで 4.1 s)、
  (b) Linux で LTO 対応 linker(lld 等)が要る、(c) `-mllvm` 系の codegen flag を
  link 側にも渡す必要がある、の 3 つ。
- 正しさ(再現性・incremental = clean)は macOS で崩れなかった。bootstrap テストは
  debug build なので `--opt` の分割の影響を受けない。
- 想定外の副産物: 20k fixture の「巨大関数の崖」(実行 1.5×、build 15×)は分割で
  消える(実行 179 → 128 ms)。ただしこれは単一 module のままでも inline 抑制で
  直せる(第1回ベンチの `noinline` 実験)ので、分割を選ぶ理由にはならない。

決定的に重いのは実行性能ではなく **toolchain 依存(U3 の (b))と、private 関数の
linkage を決める情報が IR に無いこと**(後述「実装・保守コスト」)。

## 方式ごとの比較

| | 現状維持(single unit) | 分割のみ | 分割 + ThinLTO |
| --- | --- | --- | --- |
| 実行性能(compiler 自身) | 1.00 | 1.01〜1.13× | 0.97〜1.00× |
| 実行性能(B-proj 5k / 20k / 50k) | 1.00 | 1.07 / 0.72 / 0.98 | 1.06 / 0.75 / 1.07 |
| 実行性能(B-micro 6 本) | 1.00 | 0.98〜1.00 | 0.97〜1.01 |
| compiler の cold build | 32.8 s | 9.2 s | 10.4 s |
| compiler の 1 module 編集 | 32.8 s(毎回全部) | 2.9 s | 8.0 s(LTO cache 無し)/ 4.1 s(あり) |
| 20k の cold / 増分 / no-op | 14.0 / 13.4 / 13.7 s | 1.00 / 0.34 / 0.31 s | 1.16 / 0.37〜0.59 / 0.32 s(cache あり) |
| build のピーク RSS(compiler) | 2.05 GB | 0.48 GB | 1.06 GB |
| 正しさリスク | なし | 低(macOS で再現性確認済み) | 低〜中(LTO cache と linker が増える) |
| 実装コスト | 0 | 小〜中(linkage の精密化を入れると中) | 中〜大(linker 選択、LTO cache の管理、flag の受け渡し) |

## 測定条件

| 項目 | 値 |
| --- | --- |
| マシン | Apple M4 Max 16 core、128 GB、macOS 27.0 (26A428) |
| clang / linker | Apple clang 17.0.0 (clang-1700.6.3.2)、ld64 + Xcode の libLTO |
| compiler | main `3856538` の Go seed(`go build ./cmd/kizu`)に実験パッチを当てたもの。全方式で同じ binary を使い、方式は環境変数で切り替えた。front end は shipping の self-host 版でなく Go 版(build 時間の front end 部分だけ違う。clang が支配的なので比率への影響は小さい) |
| fixture | B-proj は `bench/project/gen.py` の 5k / 20k / 50k(第3回再測定と同じ tree・CSV)、B-micro は `bench/rust/kizu/*.kizu` 6 本 |
| 実行 | 方式を交互に回して B-proj 10 回、B-micro 11 回、compiler 7 回の中央値。各方式の出力は全て byte 一致(B-proj 20k sha1 `7bc2c699`、Rust 版とも一致)。命令数は `/usr/bin/time -l` の instructions retired(1 回) |
| build | B-proj は各 3 回の中央値。cold は object cache を空にして測る。増分は毎回まだ build していない値を書く(fresh)編集で、leaf = `rules_000`、central = `text`。compiler は 1 回(cold)と 2 回(編集) |

方式の中身:

- **base**: 今の `--opt`(1 module を `clang -O3` で compile と link を 1 回)
- **split**: debug と同じ unit 分割(A1)を `--opt` に適用し、unit ごとに `-O3 -c`、
  object cache、並行 compile(A2)、通常 link
- **min**: split のうち、他 unit から参照されない関数を `internal` に戻したもの
  (後述。上限見積もり用)
- **thin**: split の各 unit を `-O3 -flto=thin -c` で bitcode にし、`-flto=thin` で link
- **thincache**: thin + ld64 の ThinLTO cache(`-Wl,-cache_path_lto,<dir>`)

## 1. 実行性能

### B-micro(6 カーネル、wall 中央値 ms)

| bench | base | split | thin | Rust | split/base | thin/base |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| fib | 19.5 | 19.4 | 18.9 | 20.5 | 1.00 | 0.97 |
| sieve | 16.9 | 16.6 | 16.6 | 17.5 | 0.98 | 0.98 |
| sum_max | 169.2 | 169.5 | 170.2 | 202.1 | 1.00 | 1.01 |
| quicksort | 448.6 | 448.8 | 450.3 | 461.8 | 1.00 | 1.00 |
| strings | 136.0 | 135.4 | 135.8 | 146.0 | 1.00 | 1.00 |
| matmul | 58.4 | 58.5 | 57.8 | 57.7 | 1.00 | 0.99 |

フェーズ別でも全て ±2% 以内。理由: 1 file の program は hot code が利用者の unit に
収まり、`Array<i64>` 等の generic instance は §3 どおり使う側の unit に
`linkonce_odr` で入るので、unit を跨ぐ呼び出しは `process::arg`・`fmt::write_i64`・
`String.deinit` の起動・終了処理だけだった(object の未定義 `kz-` symbol で確認)。

### B-proj(wall 中央値、命令数、binary の大きさ)

| 規模 | 方式 | wall | 対 base | 命令数 | binary |
| --- | --- | ---: | ---: | ---: | ---: |
| 5k | base | 114.4 ms | 1.00 | 1.503 G | 223 KB |
| | split | 122.7 ms | 1.07 | 1.981 G | 223 KB |
| | min | 123.0 ms | 1.07 | 1.981 G | 161 KB |
| | thin | 121.0 ms | 1.06 | 1.583 G | 183 KB |
| | Rust | 104.7 ms | 0.92 | 1.404 G | 550 KB |
| 20k | base | 179.1 ms | 1.00 | 3.015 G | 610 KB |
| | split | 128.1 ms | **0.72** | 1.978 G | 564 KB |
| | min | 128.0 ms | 0.72 | 1.980 G | 294 KB |
| | thin | 134.7 ms | 0.75 | 1.584 G | 590 KB |
| | Rust | 117.4 ms | 0.66 | 1.370 G | 960 KB |
| 50k | base | 136.6 ms | 1.00 | 1.504 G | 1444 KB |
| | split | 134.3 ms | 0.98 | 1.981 G | 1230 KB |
| | min | 134.3 ms | 0.98 | 1.981 G | 560 KB |
| | thin | 146.1 ms | 1.07 | 1.586 G | 1404 KB |
| | Rust | 127.4 ms | 0.93 | 1.372 G | 1745 KB |

(thincache は thin と同じ binary 性能なので省略。)

- 分割は **命令数を +32%** 増やす(5k / 50k)。module を跨ぐ `text` の helper
  (`find_byte`、`contains` 等)が LLVM で inline されなくなるため。Kizu IR の
  `Inline`(8 命令以下)は分割前にかかるので、ごく小さい関数は今も inline される。
  wall への出方は 5k で +7%、50k で −2% と命令数ほどではない。
- 20k は分割で速くなる。単一 module では呼び出し元が 1 つの関数がすべて `main` に
  畳まれて 7.3 万行の関数になる(第1回ベンチ)が、分割すると unit を跨ぐ関数は
  `hidden` で internal でないので「最後の呼び出し」ボーナスが効かず、崖を踏まない。
- ThinLTO は命令数を base に近い 1.58 G まで戻す(module を跨いで inline する)が、
  50k の wall は split より遅い(1.07×)。命令数が少ないのに遅い理由は切り分けて
  いない。候補は link 時 backend への codegen flag の伝わり方(下記 §3 の flag の件)と
  code 配置。

### compiler 自身(shipping binary に当たる。wall 中央値)

| 仕事 | base | split | min | thin |
| --- | ---: | ---: | ---: | ---: |
| `check compiler` | 1999 ms | 2015 ms(1.01) | 1979 ms(0.99) | 1939 ms(0.97) |
| `build --emit-llvm compiler` | 2552 ms | 2820 ms(**1.11**) | 2757 ms(1.08) | 2516 ms(0.99) |
| `check` 50k fixture | 171 ms | 193 ms(**1.13**) | 178 ms(1.04) | 171 ms(1.00) |
| 命令数(emit-llvm) | 45.5 G | 50.9 G | 50.2 G | 45.9 G |
| binary | 6.78 MB | 7.20 MB | 6.58 MB | 6.55 MB |

module が 30 あり module 間呼び出しの多い compiler では、分割のみの劣化が
B-proj よりはっきり出る。ThinLTO で base と同等に戻る。

### linkage の影響(min の意味)

A1 の実装は split mode で **全関数**を `hidden`(external)にしている
(`internal/llvm/units.go` `splitLinkage`)。設計 §2.2 は「unit 外から呼ばれうる関数」
だけを external にすると書いており、debug では差が出ないが `--opt` では出る:
private 関数も internal でなくなるので、inline し尽くした本体が消えない(binary が
大きくなる)、呼び出し規約の変更や引数の削除ができない。

min はこれを「実際に他 unit から参照される関数だけ external」に戻した上限見積もり。
`check` 50k の劣化は 13% → 4%、binary は compiler で 7.20 → 6.58 MB、20k fixture で
564 → 294 KB に縮む。ただし min の判定は他 unit を見て決めているので cache の安定性
規則(§2.2「その module 自身から決める」)に反し、そのままは採れない。採るには
pub かどうかと「generic body が名指す private 関数」の情報を IR に載せる必要がある
(下記 §4)。

## 2. ビルド時間

### B-proj(wall 中央値、括弧内は CPU)

| 規模 | 方式 | cold | 増分 leaf | 増分 central | no-op |
| --- | --- | ---: | ---: | ---: | ---: |
| 5k | base | 1.26 s | 0.96 s | 0.96 s | 1.00 s |
| | split | 0.58 s(1.74) | 0.17 s | 0.17 s | 0.13 s |
| | thin | 0.64 s(2.31) | 0.24 s | 0.24 s | 0.21 s |
| | thincache | 0.61 s | 0.18 s | 0.23 s | 0.13 s |
| 20k | base | 13.95 s(14.09) | 13.41 s | 13.79 s | 13.69 s |
| | split | **1.00 s**(5.14) | 0.34 s | 0.34 s | 0.31 s |
| | min | 0.94 s(4.53) | 0.34 s | 0.34 s | 0.31 s |
| | thin | 1.15 s(7.17) | 0.59 s(3.85) | 0.59 s | 0.56 s |
| | thincache | 1.16 s(7.22) | 0.37 s | 0.59 s | 0.32 s |
| 50k | base | 7.73 s(8.06) | 7.34 s | 7.29 s | 7.55 s |
| | split | 2.03 s(12.25) | 0.80 s | 0.80 s | 0.77 s |
| | min | 1.96 s(11.09) | 0.82 s | 0.84 s | 0.81 s |
| | thin | 2.44 s(17.81) | 1.41 s(9.60) | 1.39 s | 1.39 s |
| | thincache | 2.41 s(18.19) | 0.84 s | 1.52 s | 0.82 s |

比較用の Rust(第3回、`cargo build --release`): 20k cold 0.84 s 前後、増分 0.82 s 前後、
no-op 0.04 s。分割すれば `--opt` でも cold・増分は cargo release と同程度になる。

- **20k の cold が 14 s → 1 s** になるのは並列化(CPU 5 s)と崖の消滅(CPU 14 → 5 s)の
  両方。50k(崖なし)では CPU は 8 → 12 s に**増える**(unit ごとの重複 compile と
  分割の固定費)が、16 core で並ぶので wall は 1/4。
- thin(LTO cache 無し)は、変更が 1 unit でも link 時に全 unit の ThinLTO backend を
  やり直すので、増分・no-op でも CPU を cold の半分以上使う。
- thincache は leaf の編集なら split 並みに戻るが、central(全 rules が import する
  `text`)の編集は import 先の inline 判断が変わる unit が多く、thin と同じになる。
  ThinLTO の cache は「関数を import した unit」まで無効化するため、Go 的な
  「interface が変わらなければ依存を再処理しない」は ThinLTO 段では成り立たない。
- no-op で残る 0.3〜0.8 s は front end(Go seed で 20k 約 0.1 s)・unit の hash・link。
  debug の no-op(第3回 20k 0.205 s)と同じ固定費の構造。

### compiler 自身(`build --target native --opt compiler`)

| 方式 | cold wall(CPU) | 1 module 編集 | no-op | ピーク RSS | cache の大きさ |
| --- | ---: | ---: | ---: | ---: | ---: |
| base | 32.8 s(33.3) | 32.8 s | 32.8 s | 2.05 GB | 0 |
| split | 9.2 s(37.0) | 2.9 s | 2.6 s | 0.48 GB | 13 MB |
| min | 8.9 s(36.0) | — | 2.7 s | 0.47 GB | — |
| thin | 10.4 s(44.2) | 7.8〜8.3 s | 9.6 s | 1.06 GB | — |
| thincache | 9.6 s(40.7) | 4.1 s | 2.6 s | — | 29 MB + LTO cache 8.6 MB |

編集は `internal/native` の文字列 literal 1 つ(IR が変わる編集)。base の増分は
cold と同じ(cache を使わない)ので cold の値を書いた。release build(`release.yml` が
seed で `--opt` build する)の時間は 32.8 s → 約 10 s になる。

### link と LTO の追加コストの切り分け

- split の link 自体は軽い(debug の第3回で 20k 0.037 s。`--opt` の object は大きさが
  近い)。分割のみの増分時間は「front end + 編集した unit の -O3 + link」でほぼ尽きる。
- thin の追加コストは link 時の ThinLTO backend。20k で no-op 時に CPU 3.8 s、
  compiler で 41 s(wall 9.6 s)。LTO cache を当てると no-op は split と同じになり、
  残るのは編集が inline 先として波及した unit の backend だけ。

## 3. 正しさ・互換のリスク

| 項目 | 結果 / 見立て |
| --- | --- |
| 生成物の再現性 | 20k fixture で base / split / thin とも「空 cache で 2 回 build」が byte 一致、「cold と warm」も byte 一致。split で leaf を編集した warm build と空 cache の build も byte 一致(incremental = clean)。macOS のみ確認 |
| bootstrap(§5) | `TestSelfhostBootstrap` の stage1 / stage2 は `--opt` 無しで build する(`cmd/kizu/selfhost_bootstrap_test.go`)ので、`--opt` の分割は直接は効かない。規則 5(stage2 は空 cache)は object cache の仕組みが同じなので `--opt` にもそのまま適用できる。ThinLTO の LTO cache は object cache と別の場所にあるので、空 cache の規則を LTO cache にも広げる必要がある |
| 両実装の一致 | selfhost の相互検証には `build --emit-llvm --opt` の text 比較(`cmd/kizu/selfhost_test.go` の `llvm-opt-package`)がある。分割すると何を印字するか(U4)を `--opt` でも決める必要がある |
| 記号の衝突 | `kz-` 接頭辞(C 識別子に `-` が無い)は `-O3` でも変わらない。`hidden` なので binary の外とは衝突しない。最適化で名前が変わることもない(LLVM は external hidden を改名しない) |
| `linkonce_odr` と `-O3` | 各 unit の copy はそれぞれ最適化されるが、全 copy が同じ IR text・同じ flag から作られ、link 順が固定なので選ばれる copy は決定的(§5 規則 4)。LLVM は `linkonce_odr` を「正確でない定義」として扱い、他の copy と食い違いうる属性推論を本体から行わないので、unit ごとの最適化差で誤った最適化が起きることはない。ただし実行される copy は「unit 順で最初の unit で最適化されたもの」になる |
| ThinLTO の codegen flag | `--opt` の `-mllvm` 系(`-enable-tail-merge=false`、`-force-target-max-vector-interleave=4`)と `-falign-loops=32`、`-target-feature -fuse-literals` は compile 時の flag で、ThinLTO では codegen が link 時に起きるので効かない。実験では `-Wl,-mllvm,...` で link に渡した(ld64 は受け付けた)が、`-mattr=-fuse-literals` が実際に効いたかは確認していない。50k の thin が命令数のわりに遅いのはこれが一因の可能性がある |
| デバッグ情報・panic 表示 | IR に source 位置が無く debug info も出さないので、方式による差は無い。panic のメッセージは runtime の文字列。違いは symbol 名だけで、crash report や `nm` に `kz-…` の名前が出る(現状は internal で `…` の名前) |
| binary の大きさ | split は private 関数も external なので、inline し尽くした本体が残り、compiler で +6%(6.78 → 7.20 MB)。min(private を internal)なら −3% |

## 4. 実装・保守コスト

分割そのものは A1 で「partition の引数 1 つ」になっているので、経路を `--opt` に
広げる差分は小さい。実験パッチでは Go 側で次の 2 か所だけで分割のみが動いた:
`cmd/kizu/build.go` `emitUnits` の `!opt`、`internal/native/build.go` `unitInputs` の
`options.Opt` 分岐。selfhost 側の対応は `compiler/src/main.kizu`(emit_units の split
引数)と `compiler/src/internal/native/native.kizu` `unit_inputs` の `options.opt` 分岐。

実際に入れるときに要るもの:

| 項目 | 分割のみ | + ThinLTO | 両実装 |
| --- | --- | --- | --- |
| 上の分岐を外す | 要 | 要 | Go / compiler |
| private 関数を internal に戻す(min 相当を cache 安定に) | 実行性能のために要る | 推奨(binary の大きさ) | `ir.Function` に「他 module から見えるか」を足す。pub と「generic body が名指す private」— B1 の export data と同じ情報 |
| `--emit-llvm --opt` の印字(U4) | 要 | 要 | 両方 + selfhost の corpus |
| object cache key に thin を入れる、bitcode object の扱い | — | 要 | 両方 |
| link 時の codegen flag(`-Wl,-mllvm,…`) | — | 要。linker ごとに綴りが違う(ld64 / lld) | 両方 |
| LTO cache の場所・prune・`cache status`、bootstrap 規則 5 の拡張 | — | 要 | 両方 |
| Linux の linker | — | 要。既定の GNU ld は ThinLTO を読めない(LLVMgold が要る)ので `-fuse-ld=lld` 等。release.yml の ubuntu job も | 両方 + CI |
| テスト | conformance は `--opt` の分割経路を通すだけ。incremental = clean を `--opt` でも 1 本 | 同左 + Linux / Windows CI | — |
| docs | `docs/perf.md`・`architecture.md` の「`--opt` は single unit」、設計 §4・§8 | 同左 + U3 の決定 | — |

Windows(llvm-mingw)は lld なので ThinLTO は動く見込みだが未確認。

## 測れなかったもの

- Linux / Windows での再現性と ThinLTO。手元は macOS のみ。Linux は既定 linker の
  問題が先に来るので、測る前に linker の選択を決める必要がある。
- shipping の self-host compiler(Kizu 実装)での測定。実験パッチは Go seed にだけ
  当てた。生成される IR は両実装で一致させる設計(oracle)なので、実行性能の比較には
  影響しない。build 時間の front end 部分だけ Go 版の値になっている。
- 50k の thin が命令数 −20% なのに wall +9%(対 split)になる理由。
- compiler の min の 1 module 編集(min は判定が他 unit に依存するので増分の意味が
  無い)。

## 実験の作り方(再現用、checked-in しない)

実験パッチは環境変数で切り替える使い捨てで、この報告と一緒には commit していない。

- `cmd/kizu/build.go` `emitUnits`: `KIZU_EXP_OPTSPLIT` が空でなければ `--opt` でも split
- `internal/native/build.go` `unitInputs`: 同じ条件で `--opt` を object cache の経路に通す。
  `objectCacheTarget` に `exp=<値>` を足して方式ごとに object を分ける。
  `compileUnit` は `thin` のとき `-flto=thin`、`runClang` は `-flto=thin` と
  `KIZU_EXP_LTOFLAGS`(`-Wl,-mllvm,-enable-tail-merge=false -Wl,-mllvm,-force-target-max-vector-interleave=4 -Wl,-mllvm,-align-loops=32 -Wl,-mllvm,-mattr=-fuse-literals`、thincache は `-Wl,-cache_path_lto,<dir>` も)を足す
- min: `internal/llvm/units.go` `EmitUnits` で全 unit の関数から unit 外への参照
  (`ir.DirectCallees`)を集め、`KIZU_EXP_LINK=min` のとき `functionLinkage` が
  集合に無い非 instance 関数を `internal` にする

計測は、B-micro を方式交互に 11 回、B-proj の build を各 3 回(fresh 編集)、実行を
方式交互に 10 回、compiler の `check` / `emit-llvm` を方式交互に 7 回。
