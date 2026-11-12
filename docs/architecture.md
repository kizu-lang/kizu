# Kizu アーキテクチャ概観

このドキュメントは、リポジトリに初めて触れる人(人間・agent とも)向けの 1 枚地図です。
「どこに何があるか」「データがどう流れるか」を最短で掴むことを目的にします。
各領域の深掘りは末尾のドキュメント索引から辿ってください。

## 1. 全体像: shipping 実装は 1 つ

利用者が受け取る binary は `compiler/` から build した Kizu compiler です。それを
build するのは `scripts/seed.txt` に pin した前のリリース(種)で、compiler を build する
経路はこの 1 本だけです(`docs/bootstrap.md`)。`compiler/` は Go 実装の構造と挙動を
保った機械移植として作り(ADR-0130)、Go 実装は v0.6.0 を最後に削除しました。

規模感(2026-11 時点): Kizu 製 compiler 約 131k 行(`compiler/src`、`*_test.kizu` を
除く)、Kizu 製 std 約 28k 行、examples 約 25k 行。

## 2. リポジトリ地図

```
compiler/            shipping する Kizu 製 compiler(pin した種が build する)
  src/main.kizu      CLI 本体。release binary はここから build される
  src/internal/      1 directory = 1 module の、package 外へ公開しない実装
    source, token, lexer, parser, ast   入力 text・字句・構文・AST
    typ                           型の綴りを 1 度だけ parse した形(文字列手術の代わり)
    types, ownership              型検査・所有権/借用検査
    ir, llvm, wasm, native        typed SSA IR と各 backend
    project, manifest             パッケージ/モジュール解決(std も含む)、kizu.toml
    stdmethod, stdprim, stdmeta, stdtarget
                                  std の method / primitive と compiler-defined form
    staticexpr, stdmath           static 引数の整数式と、comptime の sin / cos(std::math と同じ bit)
    unsafecap                     unsafe が覆う操作の種類と診断文言
    version                       binary の名乗り(生成 source)
    fmt, diagnostic, buildcache, cimport
  tests/             frontend / IR / LLVM の corpus、MBT fixture、test harness
scripts/             種の取得と bootstrap(bootstrap.sh)、生成器、計測
lib/kizu/             release binaryが隣で読むlibrary tree
  std/src/            Kizuで書かれた標準ライブラリ
  browser/app.mjs     browser ESM出力へcopyする共通host runtime
examples/            言語機能ごとの実例(末尾に自分の case を書く)
spec/                Lean で書いた仕様と性質の証明。TLS の表は std::tls から生成し、example が再生する trace を生成する
tests/behavior/      振る舞いの assert を 1 package に束ねたもの
tests/fixtures/      module 解決などが使う固定入力
docs/tutorial/       1 つのものを最初から最後まで作る文書と、その sample package
docs/, docs/adr/     設計ドキュメントと ADR
```

## 3. パイプライン

```
source.kizu   (module は compiler/src/internal/ 以下)
  → lexer → parser → ast
  → project が import した std module を合流(std も 1 つの package)
  → build target を types / ownership / IR の comptime evaluator へ渡す
  → types(型)→ ownership(所有権)
  → ir(typed SSA)→ target の entry / export から到達可能性を閉じる
                  → llvm → clang(native)
                  → wasm → 共通 WebAssembly module
                                           → WAT / binary `.wasm`
                                             (wasm32-wasi / wasm32-browser)
                                           → browser ESM directory
                                             (`app.wasm` + `app.mjs`)
```

`run` と `test` は生成した実行ファイルを走らせます。経路は 1 本で、interpreter は
ありません(ADR-0083)。挙動の正は例そのものが末尾に書いた case が持ちます。

`llvm` は出した text を自分で検査します(`verify.kizu`)。読む register が
その関数の中で定義されていること、`alloca` が entry block にあることの 2 つで、
どちらも壊れると診断の無い実行時の失敗になります —— 前者は clang が SSA 名だけを
言って落ち、後者はループがスタックを 1 回転ごとに食って guard page で死にます。
命令の形は固定しません。

debug build の LLVM IR は module ごとの unit と、全体から決まるもの(package の
`main`、test runner、error 名の表)を持つ program unit に割ります(`llvm.EmitUnits`)。
unit の順は依存が先の module 順、最後に program unit で、関数の所属は lowerer が
宣言の module から写した `ir.Function.Module` だけで決めます。unit を跨いで呼ばれうる
関数は `hidden` で、symbol は `kz-` で始まります。`-` は C の識別子にも Kizu の
symbol 名にも現れないので、unit 外へ出しても runtime の libc 呼び出しを横取りしません。
generic instance は使う側の unit がそれぞれ `linkonce_odr` で持ち、linker が 1 つ残します
(ELF / COFF は comdat、Mach-O は weak 定義の合併)。`--opt` は 1 unit のままです。
`--opt --thinlto` は同じ割り方で(`llvm.PerModuleLTO`)、他の unit が名指す関数・
address を取られる関数・C export だけを `hidden` にし、残りを `internal` にします。
最適化が inline し尽くした本体を消し、呼び出し規約を変えられるようにするためで、
そのぶん unit の text は他の unit がどの関数を呼ぶかにも依存します。各 unit は
`source_filename` に module 名を書きます。ThinLTO は internal な関数を source file 名と
名前で区別するためです。

unit の object と実行ファイルは build cache に残ります。鍵は LLVM IR・runtime の
source・toolchain、つまり**何でできているか**だけで、ファイル名も時刻も入りません。
front end は毎回通り、text が変わらない unit の compile と、全 unit が変わらなければ
link を飛ばします。object と runtime object の鍵にはさらに `clang --version` の 1 行目が
入ります。clang を替えても path と flag は同じなので、そこだけが古い object と新しい
clang が作る object を区別します。この問い合わせは link するときだけで、cache にある
実行ファイルを走らせるだけの `run` は toolchain に触れません。
桁の目安は `kizu run examples/hello.kizu` で初回 ~0.5s(うち大半が clang の
link)、2 回目以降 ~10ms —— 絶対値は host と clang によります。
`build --target native` は利用者が名前を指定した成果物とその build 記録を書くコマンド
なので実行ファイルは cache から読まず毎回 link しますが、unit の object は cache から
読みます。selfhost の bootstrap 検査は第 2 段を空の cache で build し、object を
compile し直した結果が第 1 段と一致することを見ます。

`wasm` は typed SSA IR を 1 度だけ WebAssembly module へ lower し、その同じ
module を inspection 用 WAT と deterministic な binary `.wasm` に描画します。WASI は
`wasmtime`、browser target は JavaScript host adapter で同じ conformance output を検査し、
target 差は import、entry / export、host capability だけが持ちます。
`--emit esm` はそのbrowser binaryと`lib/kizu/browser/app.mjs`を同じdirectoryへ置くだけで、
backendを増やしません。moduleのimportだけではinstantiateも`main`も始まりません。

同じ package に target 別 adapter がある場合、`std::target` を条件にした
`comptime if` は選ばれた branch だけを type / ownership / IR が扱います。その後、
native は `main` と `export "c"`(`--emit obj` は `export "c"` だけ)、WASI は `main`、
browser は `main` と `export "browser"` から到達できる関数だけを backend へ渡します。実行例は
[`examples/modules/target_adapters`](../examples/modules/target_adapters) です。

`wasm32-wasi` の host boundary は WASI Preview1 です。blocking stdin / stdout / stderr、
引数・環境・時刻、preopen された filesystem は、その capability を example の metadata
から Wasmtime へ渡して再現します。子 process、socket / `std::net` と `std::http` の network 経路、evented
`std::io` / `std::coro`、extern C はこの boundary に無いため、到達する program を module
描画前に target 非対応として拒否します。browser 側の対応範囲と JavaScript ABI は
[`docs/wasm-browser.md`](wasm-browser.md) が持ちます。

CLI のコマンド: `run` `parse` `check` `test` `fmt` `init` `ir`
`build`(`--emit-llvm` / `--target native|wasm32-wasi|wasm32-browser`、nativeの
`--emit obj`、browserの`--emit esm`)`cache` `import-c-header`
`version`。基本の実行経路は `kizu run examples/hello.kizu`。どのコマンドがどこへ
流れるかは `compiler/src/main.kizu` が全てです。

## 4. std の二層構造

- 実行時の組み込み(stdout への 1 行書き込み、メモリ、fs、process、net、coro 等)は compiler が
  提供し(`stdprim`)、
  `lib/kizu/std/src/*/*.kizu` はその上の Kizu 製 API 面(`std::array` `std::map`
  `std::string` `std::http` …)。境界の一覧は docs/stdlib.md。
- std の宣言は `project` が取り込みます。std は利用者の package と同じ loader を
  通ります。
- std が宣言する method 署名は `stdmethod` が、その裏の `std::internal::builtin::*`
  primitive の形は `stdprim` が一本化します。checker と backend は署名を
  書き直さずにここを読みます。

## 5. テスト体系

言語の正しさは **examples + tests/behavior** が持ちます。どちらも自分が何を
約束するかを末尾のコメントブロックに書き、conformance test が木を歩いてそれを
読みます。登録簿はないので、登録し忘れという状態が存在しません。

| 層 | 実行方法 | 内容 |
| --- | --- | --- |
| 種からの build | `just selfhost`(= `scripts/bootstrap.sh`、pre-push hook と CI) | pin した前リリース(種)で `compiler/` を check / test し、種が build した stage1 でもう一度 check / test する。`docs/bootstrap.md` |
| Kizu 側 unit test | 同上(`kizu test compiler`) | `compiler/src/**/*_test.kizu`。`compiler/tests/` の corpus と MBT fixture の再生もここ |
| 生成物と整形 | 同上(`scripts/check-sources.sh`) | checked-in の Kizu source が `kizu fmt` の形であること、std::tls から生成する spec の表が最新であること |
| behavior | 同上(`kizu run compiler/tests/harness -- behavior <kizu>`) | conformance case のうち CLI 境界を越える実行の振る舞い(`tests/behavior/`、test 系と std-io / std-process の example)。harness 自身の test は `kizu test compiler/tests/harness` |
| conformance | `just conformance`(`kizu run compiler/tests/harness -- conformance <kizu>`)。`just selfhost` には含まない | `examples/`、`tests/behavior/`、`docs/tutorial/` の全 case。behavior はこの部分集合 |
| bootstrap | 同上(`kizu run compiler/tests/harness -- bootstrap <stage2>`) | stage1 が build した stage2 が、自分自身を空の build cache で build し直して byte 単位で同じ実行ファイルになること |
| commit hooks | `pre-commit run --all-files` | 種と stage1 による `kizu check compiler`(`scripts/bootstrap.sh --check-only`)。数秒で終わるものだけ |

CI は push/PR ごとに、必須 job `test (ubuntu-latest)` と `test (macos-15)` で
`scripts/bootstrap.sh` を全部回します。定時実行は置きません。
言語機能を足したら `examples/` に実例を足し、その末尾に case を書きます。
書式は `compiler/tests/harness/src/conformance/conformance.kizu` の冒頭にあります。

## 6. 開発フロー

- 作業は topic branch + PR(`main` 直 push 禁止)。PR には目的・主要変更・検証結果を書く。
- コミット時に hooks(種と stage1 の selfhost check)、push 時に
  `scripts/bootstrap.sh` のフル版が走る。
- 仕様に関わる判断をしたら SPEC.md か docs/adr/ を必ず更新する。

## 7. オンボーディング順路(読む順)

1. `README.ja.md` → `SPEC.md` の冒頭と §13 あたりまで(言語像)
2. `examples/hello.kizu` を `kizu run` / `check` / `build --emit-llvm` で触る
3. `compiler/src/main.kizu`(CLI の入口)
4. `compiler/src/internal/types` と `compiler/src/internal/ownership`(何が拒否されるか)
5. `compiler/src/internal/ir` → `compiler/src/internal/llvm`(生成系)
6. 必要になった領域の docs/ と docs/adr/(下の索引)

## 8. ドキュメント索引

| 知りたいこと | 場所 |
| --- | --- |
| 言語仕様 | SPEC.md |
| 開発ルール・禁止事項 | AGENTS.md |
| メモリ安全モデル | docs/memory-safety.md |
| std の API リファレンス | docs/std/ |
| 作りながら覚える(web server など) | docs/tutorial/ |
| trusted builtin の境界と std API 追加の規則 | docs/stdlib.md |
| build / cache 性能の測り方 | docs/perf.md |
| 種の pin と更新手順、2 リリースがかりの規律 | docs/bootstrap.md |
| self-host の移植を選んだ理由 | docs/adr/0130-selfhost-by-mechanical-porting.md |
| 移植で見つかった言語 / std の gap | docs/language-gaps.md |
| API の形の選び方(不在 / 失敗 / バグ) | docs/style.md |
| 主要な設計判断(IR/型/所有権/comptime …) | docs/adr/(特に 0006 comptime、0009 IR、0014 typed SSA、0049 モジュール解決)|
