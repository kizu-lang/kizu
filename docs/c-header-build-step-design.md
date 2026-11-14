# C header 取り込みをビルドステップにする(設計)

issue #745 で、C header の取り込みはコンパイラ内蔵(Zig 0.15 以前の `@cImport` 型)ではなく
**ビルドステップ**にすると決まった。この文書は、それを Kizu でどういう形にするかの提案である。
実装・SPEC 変更は含まない。決まった部分は実装と同時に `SPEC.md` / ADR へ移し、
この文書は役目を終えたら消す。

## 0. 推奨(要約)

| 問い | 推奨 | 主な理由 |
| --- | --- | --- |
| 翻訳はどこで走るか | `kizu.toml` の `[c.<name>]` 宣言を、package を読む全コマンド(`check` / `run` / `test` / `build`)が project load の一段として回す。`build.kizu` は作らない | SPEC §3・ADR-0024・`docs/perf.md` が build script を持たないと決めている。生成物のチェックインは target ごとの差と陳腐化を利用者に負わせる |
| 生成の実体 | **前処理は clang(`clang -E -dD -MD`)に任せ、宣言の翻訳は既存の `cimport` を育てた自前の translator**(Go と `compiler/` の両方) | native build は既に host clang に依存している(ADR-0052)。clang の JSON AST は形式が安定しないと明記されている。前処理器を自作すると target の predefine を再現できない |
| 対象範囲 | 関数(可変長含む)、typedef(展開)、`extern "c" struct` で書ける struct、不完全 struct、enum の値、関数 pointer、literal だけの object-like macro。それ以外は理由付きで飛ばす | Kizu が書けるものだけを出す。union・bitfield・配列 field・module 定数は Kizu 側に受け皿が無い(§4.3) |
| キャッシュ | clang の前処理出力だけを content-addressed な artifact として cache する(depfile で全 include を key に入れる)。翻訳した Kizu text は cache せず毎回作る | ADR-0126(cache は道具が作った artifact だけ)と一致する。翻訳は front end と同じく安い |
| リンク指定 | library 名は manifest の import 宣言に書き、translator が生成する全 `extern "c" fn` に `@link_library` / `@link_framework` を付ける。探索 directory は今の `[native]` のまま | SPEC §12.2 の「名前は宣言の上、在処は manifest」をそのまま保つ。生成物を読めば依存が見える |
| import の形 | 生成物は `<package>::<name>` の通常の module として見え、`import app::sqlite;` で読む。source directory と同名なら manifest error | module 解決に特例を作らない |
| 増分・no-op | warm の no-op は header の再 hash + 翻訳 + 今の front end。clang は起動しない | header が変わっても宣言が同じなら IR unit が同じなので object は hit する |

## 1. 参照実装

### 1.1 Zig 0.16: `@cImport` の廃止と `b.addTranslateC`

0.16.0 の release notes は `@cImport` を deprecated とし、C の翻訳は今後 build system が
持つと書いている。同じ版で translate-c は libclang から arocc ベースに置き換わり、
C++ 5,940 行が消えた。

https://ziglang.org/download/0.16.0/release-notes.html

廃止の提案(#20630、accepted、2026-04 close)の理由:

* compiler の実行ファイルから libclang への依存を 1 つ外す(LLVM を library 依存から
  process 依存へ移す流れの一部)。
* 利用者を `std.Build.Step.TranslateC` に先に移しておく。
* build step なら名前空間の剥ぎ取りや binding の検証といった機能を、言語を変えずに足せる。
* 反対意見として「どの libc を使うかは header の利用者が決める」がスレッドに出ている
  (= 翻訳の入力は target と libc 選択に依存する)。

https://github.com/ziglang/zig/issues/20630

build graph 上の形:

```zig
const translate_c = b.addTranslateC(.{
    .root_source_file = b.path("src/c.h"),
    .target = target, .optimize = optimize,
});
translate_c.linkSystemLibrary("glfw", .{});
// exe の root_module の .imports に .{ .name = "c", .module = translate_c.createModule() }
// main.zig では const c = @import("c");
```

* 翻訳 step の入力は header 1 つ + include path / framework path / define(step の method)。
  出力は 1 つの `.zig` で、`createModule()` が private module、`addModule(name)` が
  依存先にも見える module にする。
* 生成物は `.zig-cache/o/<digest>/<stem>.zig`。cache manifest は header と translate-c の
  tag を hash し、Aro が出した depfile(`.d`)を `addDepFilePost` で取り込んで**読まれた
  全 header** を manifest に記録する。どれかが変われば miss して翻訳し直す。
* link 指定は半分だけ分かれている。`TranslateC.linkSystemLibrary` は pkg-config の
  cflags を翻訳に渡しつつ、作った module の link 対象にも library を写す。
  framework や library path は従来どおり使う側の module / compile step に書く。

https://codeberg.org/ziglang/zig/src/tag/0.16.0/lib/std/Build/Step/TranslateC.zig
https://codeberg.org/ziglang/zig/src/tag/0.16.0/src/main.zig
https://codeberg.org/ziglang/zig/src/tag/0.16.0/src/Compilation.zig

translator 本体は独立 package(arocc ベース)になった。object-like macro は `pub const`、
function-like macro は `anytype` 引数の `inline fn`、訳せない macro は使ったときだけ
compile error になる宣言、bitfield を持つ record・layout の無い record・訳せない field を
持つ record は opaque に落とす。

https://codeberg.org/ziglang/translate-c/src/branch/main/README.md
https://codeberg.org/ziglang/translate-c/src/branch/main/src/Translator.zig
https://codeberg.org/ziglang/translate-c/src/branch/main/src/MacroTranslator.zig

### 1.2 Rust: bindgen + build.rs

* build.rs は package の build 直前に走る。`rerun-if` を出さなければ package 内の
  どの file が変わっても再実行。`cargo:rerun-if-changed=PATH` は **mtime** で比べる。
  何も変わっていなければ前回の出力(link 指示など)を再利用する。
* bindgen の `CargoCallbacks` は include された全 file に `rerun-if-changed` を、読んだ
  環境変数に `rerun-if-env-changed` を出す。
* 生成物は `OUT_DIR/bindings.rs` に書き、`include!(concat!(env!("OUT_DIR"), "/bindings.rs"))`
  で取り込む。
* link は build.rs が `cargo:rustc-link-lib=[KIND=]NAME`(dylib / static / framework)と
  `cargo:rustc-link-search=[KIND=]PATH` を出すか、source の `#[link(name = ...)]` に書く。
  `links = "z"` と `*-sys` crate の慣習で 1 つの native library を 1 package に寄せる。
* bindgen は libclang(Clang 9 以上)で前処理・parse・型検査を行う。build 時生成は
  「今の target 用」の binding を得る代わりに、build する全員が libclang を要する。
  事前生成は libclang を不要にするが target ごとに file を持つことになり、guide は
  これを "a huge pain" と書く。Cargo は build.rs から checked-in file を生成せず、
  再生成して diff を検査する test を CI に置くよう勧める。
* enum は既定で定数群(`EnumVariation::Consts`)。Rust enum にすると不正値が UB になる。
  bitfield は getter / setter 付きの struct、opaque 型は size / align だけを持つ byte 列。
  inline 関数は symbol が無いので既定では出さない。macro は int / str の callback があり、
  function-like macro は callback で見えるが翻訳には影響しない。

https://doc.rust-lang.org/cargo/reference/build-scripts.html
https://doc.rust-lang.org/cargo/reference/build-script-examples.html
https://rust-lang.github.io/rust-bindgen/requirements.html
https://rust-lang.github.io/rust-bindgen/tutorial-4.html
https://rust-lang.github.io/rust-bindgen/library-usage.html
https://docs.rs/bindgen/latest/bindgen/struct.CargoCallbacks.html
https://docs.rs/bindgen/latest/bindgen/enum.EnumVariation.html
https://doc.rust-lang.org/reference/items/external-blocks.html

### 1.3 Kizu に持ち込むもの・持ち込まないもの

| 持ち込む | 持ち込まない |
| --- | --- |
| 翻訳を target 依存の build 入力として扱う(両者) | 任意コードを走らせる build script(build.rs / build.zig)。Kizu は §2.2 の理由で持たない |
| depfile で読まれた全 header を cache の依存に入れる(Zig) | mtime による再実行判定(Cargo)。Kizu の cache key は中身だけ(`docs/perf.md`) |
| 訳せないものは opaque に落とすか飛ばし、使ったときに理由が分かる(Zig) | function-like macro の翻訳。Kizu には `anytype` も macro も無い |
| enum を定数群にする(bindgen 既定) | C の enum を Kizu の `enum` にする。範囲外の値が来うる |

## 2. Kizu の現状

### 2.1 `kizu import-c-header`

* 実装は Go の `internal/cimport/importer.go`(224 行)と、その機械的移植
  `compiler/src/internal/cimport/cimport.kizu`(686 行)。正規表現と `;` 区切りの
  文字列処理で、C の parser も preprocessor も持たない。clang / libclang に依存しない
  (ADR-0008 の決定)。
* 対応は function prototype と primitive・単純な pointer だけ(SPEC §12.2)。typedef、
  struct / enum / union、可変長、関数 pointer、配列引数、macro、`#` 行は拒否し、
  1 つ目の拒否で import 全体が止まる。
* 出力は stdout に印字するだけで、build とも cache ともつながっていない。SPEC は
  「build script ではない」「将来 cache に入れるなら key は importer version、header path、
  content hash、target ABI、option」と書いている。
* Quint モデル(`spec/cimport/`、e738bf7)と MBT が、現行 importer に 3 つの受理側の
  食い違いを見つけている(`findings/cimport-quint-findings.md`): **F1** `const` の位置を
  見ない、**F2** pointer の結果を non-null の `ptr<T>` にする、**F3** pointee の
  `volatile` を落とす。新しい translator はモデルの「C 型の真の対応」を規則として持ち、
  この 3 つを直した写像を出すべきである(§4.2)。

### 2.2 前提だった機能はどこまで揃ったか

| 機能 | 状態 | 出所 |
| --- | --- | --- |
| `extern "c" fn`、可変長引数 `...` | 確定 | SPEC §12 |
| `extern "c" struct`(field は整数・浮動小数・`bool`・raw pointer・別の extern struct) | 確定 | SPEC §12.2、ADR-0024 |
| struct の値渡し(arm64 / x86-64 SysV / Win x64) | 確定 | ADR-0153 |
| C 関数 pointer `extern "c" fn(...) -> T`、`?extern "c" fn` | 確定 | SPEC §12.4、ADR-0151 |
| `@link_library` / `@link_framework`、`[native]` の探索 directory | 確定 | SPEC §3・§12.2、ADR-0024 |
| **module 定数**(top-level の `const`) | 無い。parser の top-level は fn / struct / enum / union / contract / error set のみ | `internal/parser/parser.go` `parseTopLevelDeclWithDoc` |
| **opaque な extern 型**(不完全 struct) | 無い | — |
| **untagged union**、**bitfield**、**struct 内の固定長配列** | 無い(extern struct の field に配列を書けない) | SPEC §12.2 |
| **extern 変数**(C の global) | 無い | — |

下 4 行が translator の範囲を決める(§4.3)。

### 2.3 build の流れと cache

* `kizu.toml` は宣言的 TOML で「build script、plugin、条件分岐、実行可能な設定は
  書けない」(SPEC §3)。今の key は `[package]`、`[modules].paths`、`[native]` /
  `[native.<os>]`(`internal/manifest`)。
* `docs/perf.md` の cache 設計の制約に「build script と proc macro による隠れた依存を
  作らない」がある。ADR-0024 は「Kizu は build script を持たないので、[探索 directory は]
  宣言的な manifest の表に置く」と書いている。
* front end は毎回全体を通る。cache が持つのは clang が作るもの(runtime object、
  unit object / bitcode、実行ファイル)だけで、key は IR text・runtime source・toolchain の
  中身から作る(ADR-0126、`docs/architecture.md` §3)。warm の `run` は toolchain に
  触れず ~10ms。
* link は clang driver(ADR-0052)。到達可能性を閉じた後に残った extern の
  `@link_library` だけを `-l` にする。
* project loader は既に「path → source text」の上書き(`LoadProgramWithSources`)を
  持つ。生成 module をここへ流し込めば parser 以降は通常の module と同じ経路になる。
* `@embed`(issue #610、未実装)は「build cache は埋め込み file の内容変化を追跡して
  再 build する」としている。C header の依存追跡は同じ問題で、§5 の形を共有できる。

## 3. 翻訳はどこで走るか

| 案 | 内容 | 利点 | 欠点 |
| --- | --- | --- | --- |
| **A. manifest 宣言 + compiler が回す**(推奨) | `kizu.toml` に header と翻訳の option を書き、package を読むコマンドが project load の中で翻訳する | 宣言的で、何が C 由来かが manifest に全部ある。target を知っている build が翻訳するので target 差が自然に入る | 翻訳 option の語彙を manifest に足す必要がある。`kizu check` も clang を要するようになる |
| B. 生成物をチェックイン | `kizu import-c-header` の出力を source に置く | build が clang を要さない | bindgen guide の言う target ごとの file 管理が利用者に来る。header 更新との同期を人が覚える(原理 1: 傷を隠す)。陳腐化を検出する test を各 package が書くことになる |
| C. `build.kizu` | Kizu で書いた build program が翻訳 step を組み立てる | Zig / Cargo と同じ表現力 | SPEC §3・ADR-0024・perf.md の制約と正面から矛盾する(原理の変更になる)。build のたびに build program を compile して走らせる経路が増え、no-op build が遅くなる。任意の副作用が cache key の外に出る |

**推奨は A。** C はこの文書の範囲では選べない(`docs/principles.md` に照らすと、
ユーザーが原理を変える判断になる)。B は禁止しないが推奨経路にはしない —— 出力を手で
置くのは「Kizu source を書く」ことであって別の compiler 経路ではないので、原理 9 には
触れない。`kizu import-c-header` は manifest の 1 宣言を指定して、A が build に流すのと
**同じ text** を印字する inspection コマンドに変える(`kizu ir` と同じ位置づけ)。

manifest の形(綴りは案):

```toml
[c.sqlite]
header = "vendor/sqlite/sqlite3.h"   # manifest からの相対
include = ["vendor/sqlite"]          # -I
define = ["SQLITE_OMIT_DEPRECATED"]  # -D、値付きは "NAME=1"
link_library = "sqlite3"             # 生成する全 extern fn に付ける(§6)

[c.sqlite.darwin]                     # [native.<os>] と同じ規則で OS ごとに足す
include = ["/opt/homebrew/include"]
```

* 1 entry = 1 header = 1 module。複数 header をまとめたいときは、利用者が `#include` を
  並べた header を 1 つ書く(Zig の `src/c.h` と同じ)。
* target(triple、OS)は `kizu build` が選んだものを clang の `-target` に渡す。
  manifest には書かない。
* wasm target は extern C を拒否する(architecture.md §3)ので、C module を import した
  program は今と同じ到達性の検査で落ちる。翻訳自体は native target でだけ走らせる。

## 4. バインディング生成の実体

### 4.1 前処理と parse をどこに任せるか

| 案 | 依存 | 評価 |
| --- | --- | --- |
| a. 今の importer を育て、前処理も自作する | 無し | 実用の header は system header を `#include` し、`#if` で target を分ける。target の predefine(`__aarch64__`、`__APPLE__`、`__SIZEOF_LONG__` …)と SDK の探索を clang と一致させる手段が無い。Go と Kizu の両方に C preprocessor を書くことになる |
| **b. `clang -E` で前処理し、宣言の翻訳を自作する**(推奨) | host clang(process) | clang は native build で既に必須(ADR-0052)なので新しい依存ではない。前処理は GCC 互換で長く安定した driver flag(`-E -dD -MD -MF`)だけを使う。翻訳器は宣言の parser で済み、関数本体は brace を数えて飛ばせる |
| c. `clang -Xclang -ast-dump=json` を読む | host clang | 型・enum 値・typedef を clang が解決してくれる。しかし clang 自身が JSON dump の形式に major release 間の安定性を約束しない(`JSONNodeDumper.h` の comment、D60910 の議論)。型は結局 `qualType` 文字列で来るので C の型綴りの parser も要る |
| d. libclang | C library を compiler に link | Go は cgo、Kizu は C library の link を compiler の build に足すことになる。Zig が外した依存そのもの |

https://clang.llvm.org/doxygen/JSONNodeDumper_8h_source.html
https://lists.llvm.org/pipermail/cfe-commits/Week-of-Mon-20190603/274362.html

**推奨は b。** 手元の計測(Apple clang 17、M 系 mac)で `clang -E -dD -MD` は
`<stdio.h>` だけの header、SDK の `sqlite3.h`、`zlib.h` のどれも 20ms 前後、出力は
240〜320KB、depfile の header 数は 12〜115。翻訳器が読む量は compiler の 1 module と
同じ桁である。

翻訳器の入力は前処理後の text で、次のように読む。

* `# <line> "<file>"` の line marker で各宣言の出所 file を知る。
* **出す宣言は manifest の `header` に書かれた file(と同じ entry の `include` 配下)由来の
  ものだけ**、加えてそれらが参照する型を推移的に閉じたもの。system header の宣言は
  参照されたときだけ出る(bindgen の allowlist を「宣言 file」で自動に決める形)。
  明示の allowlist / blocklist は実需が出るまで足さない(原理 8)。
* system header 由来の宣言は寛容に読む: typedef と struct は解釈を試み、読めない宣言は
  `;` か対応する `}` まで飛ばして「不明」と記録する。宣言 file 由来の宣言が不明な型を
  参照したら、その宣言を理由付きで飛ばす(§4.4)。
* `-dD` が残す `#define` 行から object-like macro を拾う。

Go と `compiler/` の両方に同じ翻訳器を置く(AGENTS.md)。clang の出力は両実装で同じ
bytes なので、translator の出力を corpus で byte 比較できる。今の importer の MBT
(`spec/cimport/`)はそのまま新しい translator の検査として拡張する。

### 4.2 写像の規則(現行からの変更点)

* `const` / `volatile` は位置どおりに読む(F1、F3)。Kizu に `volatile` の受け皿が無いので、
  pointee が volatile な引数・結果を持つ宣言は飛ばす。
* pointer の結果は `?ptr<T>`(F2)。引数の pointer も C は NULL を許しうるので
  `?ptr<T>` にするか `ptr<T>` にするかは**未決 Q4**。
* typedef は Kizu に型 alias が無いので展開する。`size_t` などの名前は失われるが、
  生成 text に `// size_t` の comment を残す。
* `long` / `unsigned long` / `_Bool` / `uintptr_t` は clang の predefine(`__SIZEOF_LONG__`
  など)から幅を決める。今の importer が拒否している綴りである。
* 無名引数は `p<i>`。Kizu の keyword と衝突する名前は同じ規則で付け替える(今と同じ)。

### 4.3 対象範囲

| C の構成 | 出すもの | 前提 |
| --- | --- | --- |
| 関数宣言(可変長を含む) | `extern "c" fn` | 確定済み |
| `static inline` 関数 | 飛ばす(symbol が無い。bindgen 既定と同じ) | — |
| typedef | 展開(§4.2) | — |
| 全 field が書ける struct | `extern "c" struct` | 確定済み(ADR-0024 / 0153) |
| 関数 pointer の引数・field・typedef | `extern "c" fn(...) -> T`、`?extern "c" fn(...)` | 確定済み(ADR-0151) |
| 不完全 struct(`struct sqlite3;`)、書けない field を持つ struct | opaque 型 | **言語側に opaque な extern 型が要る**(未決 Q2) |
| enum | 定数群(bindgen 既定と同じ。Kizu の `enum` にはしない) | **module 定数が要る**(未決 Q1) |
| object-like macro(整数・浮動小数・文字列 literal、それらの単純な式) | 定数 | 同上 |
| function-like macro | 飛ばす | Kizu に macro も `anytype` も無い |
| union、bitfield、struct 内の固定長配列 | それを含む struct を opaque に落とす | 受け皿を足すかは別判断 |
| extern 変数(C の global) | 飛ばす | 受け皿を足すかは別判断 |

module 定数が無いうちの代替は `pub fn NAME() -> i32 { return 3; }`(std が既に使う形)
だが、生成物の綴りが言語に定数が入った時点で変わる(breaking)。Q1 で先に決めたい。

### 4.4 訳せなかったもの

Zig は訳せない宣言を「使ったら compile error になる宣言」として残す。Kizu で同じことを
するには「存在するが使えない宣言」という新しい種類が要る。

推奨: 生成 module には出さず、**飛ばした名前と理由の表を translator の結果に持つ**。
resolver が C module の未知の名前に出会ったら、その表を引いて
「`sqlite3_vfs` は bitfield を持つため翻訳していません」と診断する。
`kizu import-c-header` の出力でも同じ表を comment で末尾に印字する。

## 5. キャッシュと無効化

### 5.1 何を cache するか

| 案 | 評価 |
| --- | --- |
| 翻訳した Kizu text を header の hash で cache | ADR-0126 が廃止した text cache と同じ形。key に translator(= compiler)の同一性が入らないので、compiler を替えても古い翻訳が返る。build-cache 設計の未決 U1(compiler identity)が決まるまで作れない |
| **clang の前処理出力を cache し、翻訳は毎回**(推奨) | cache するのは toolchain が作った artifact だけ(runtime object と同じ扱い)で、ADR-0126 と整合する。翻訳は front end と同じく安く、compiler が変われば翻訳結果も自然に変わる |
| cache しない | 1 header ごとに clang を ~20ms 起動する。warm の no-op `run`(~10ms)が数倍になる |

### 5.2 key

前処理の結果は**読まれた全 header** の中身に依存するが、どれが読まれるかは
前処理してみるまで分からない。Zig の cache manifest と同じ 2 段にする。

1. 一次 key: `clang` の path と `--version` の 1 行目、`-target`、manifest entry の
   option(`include` / `define` の並びそのもの)、宣言 header の中身の hash。
2. entry の中身: 前処理出力と、depfile から取った「読まれた header の path と中身の
   hash」の列。
3. hit の条件: 列の全 file を読み直して hash が一致すること。1 つでも違えば clang を
   回して entry を書き直す。

* `docs/perf.md` の「key に source path を入れない」は守る(path は key ではなく entry の
  中身にあり、比べるのは中身の hash)。mtime は使わない。
* `clang --version` を一次 key に入れると、warm の `run` も toolchain に 1 回触れる
  (計測 ~10ms)。system header は SDK / clang と一緒に変わり、その中身は 3. で比べて
  いるので、版を外せるかは **未決 Q5**。
* 新しい artifact 種別(`c-pre` など)として、`cache status` の表示・prune・
  `just perf-cache` の測定を同じ変更で足す(perf.md の制約)。
* `@embed`(#610)の依存追跡もこの「entry に依存 file の hash 列を持つ」形に乗せられる。
  `@embed` は依存が source から静的に分かるので 2 段は要らないが、比べ方を共有できる。

### 5.3 増分・no-op build

| 状況 | 起きること |
| --- | --- |
| 何も変えずに `run` | 依存 header を読んで hash(数十〜百 file)→ cache の前処理出力 → 翻訳 → front end → 全 unit の IR が同じなので実行ファイル hit。clang は起動しない(Q5 次第で `--version` 1 回) |
| Kizu の source だけ変えた | 上と同じ翻訳の後、変わった unit だけ compile(今と同じ) |
| header の comment だけ変えた | 前処理が miss して clang ~20ms。翻訳結果の text は同じなので C module の IR unit は同じ → object hit |
| header の宣言を変えた | C module の unit と、それを import する module のうち署名が変わったものを参照する unit が compile し直し |
| target を変えた | 一次 key が変わり前処理から |

C module は通常の module なので、build-cache 設計の B1(interface hash)が入れば
「C header の変更で依存 module を再処理するか」も同じ仕組みで決まる。

## 6. リンク指定の置き場所

SPEC §12.2 の規則(名前は `extern "c" fn` の上の attribute、在処は `[native]`、
到達した宣言の library だけ link)は変えない。

| 案 | 評価 |
| --- | --- |
| **manifest の C entry に `link_library` / `link_framework` を書き、translator が生成する全 `extern "c" fn` に attribute を付ける**(推奨) | 生成 text を読めば各関数の依存が見える(`kizu import-c-header` で確認できる)。到達性で link を絞る今の仕組みがそのまま効く |
| 生成物は attribute 無しで、利用者が wrapper 側で宣言し直す | 定型が call site 分増える(原理 10) |
| header の `#pragma comment(lib, ...)` などから推測 | ADR-0024 が却下済み(依存が暗黙になる) |

* 1 header が複数 library にまたがる場合は、header を分けて entry を分ける。
  1 entry に複数 library を書いて全関数に付けると、使わない library も link される。
* 探索 directory は今の `[native]` / `[native.<os>]` がそのまま持つ。翻訳用の
  `include` とは別物(header の在処と library の在処)。
* pkg-config 連携は ADR-0024 が却下済み(必要になったら `[native]` に足す)。

## 7. import の形

* `[c.sqlite]` は `<package>::sqlite` という production module を作る。同じ module path を
  source directory が作るなら manifest error。
* 生成宣言はすべて `pub`。他 package から見えるかは、通常の module と同じく
  `internal` directory 規則に従う —— 生成 module を `internal` に置きたいなら、名前に
  `internal::sqlite` のような path を許す(綴りは実装で決める)。
* 利用者は `import app::sqlite;` で読み、safe な wrapper を自分の module に書く
  (bindgen の `*-sys` と同じ分け方を、package 内の module で行う)。
* LSP は project load と同じ経路で生成 module を見るので、定義へ飛ぶ先は生成 text に
  なる。生成 text を disk に書いて開けるようにするかは実装時に決める。

## 8. 進め方(実装するときの順)

1. 言語側の受け皿(Q1 module 定数、Q2 opaque extern 型)を決める。決まらなければ
   関数・struct・関数 pointer だけで始める。
2. translator を `internal/cimport` / `compiler/src/internal/cimport` に作り直す
   (前処理後 text を読む宣言 parser、F1〜F3 を直した写像)。Quint モデルの
   「真の対応」を広げて MBT で検査する。
3. manifest の `[c.<name>]`、project load への流し込み、`import-c-header` の inspection 化。
4. 前処理 artifact の cache(§5)。
5. 検証は `examples/` に C library(libc の一部と SDK 同梱の zlib など、CI の両 OS で
   入るもの)を使う package を置き、実行結果で見る。

## 9. 未決(ユーザー判断が要るもの)

| ID | 問い | 選択肢 | 判断材料 |
| --- | --- | --- | --- |
| Q1 | enum 値と macro 定数をどう表すか | (a) module 定数を言語に足す (b) 0 引数の `pub fn` で出す (c) 当面出さない | (b) は後で (a) に移ると生成物の綴りが変わる。定数は C 以外(std の表など)にも効く言語判断 |
| Q2 | 不完全 struct と訳せない struct をどう表すか | (a) opaque な extern 型を足す(`extern "c" opaque Foo;` 相当) (b) `ptr<u8>` に潰す (c) 空の `extern "c" struct` | (b) は型の区別を失う(`sqlite3*` と `sqlite3_stmt*` を取り違えても通る)。(c) は値で作れてしまう |
| Q3 | 翻訳で clang を使うことを受け入れるか | (a) 受け入れる(推奨 §4.1 b) (b) ADR-0008 の「clang に依存しない限定 parser」を守り、前処理済み header か手書きの宣言だけを受ける | (a) は ADR-0008 の当該行を書き換えることになる。clang は native build で既に必須。`kizu check` も C module があると clang を要する |
| Q4 | C の pointer 引数を `ptr<T>` と `?ptr<T>` のどちらにするか | (a) `?ptr<T>`(C の型どおり) (b) `ptr<T>`(呼び出し側が楽) (c) nullability 注釈(`_Nonnull` 等)があれば従い、無ければ (a) | 結果側は F2 で `?ptr<T>` が妥当。引数側は Kizu から null を渡せないと呼べない API(`free(NULL)` 等)がある |
| Q5 | 前処理 cache の key に `clang --version` を入れるか | (a) 入れる(warm run が clang を 1 回起動) (b) 入れず、depfile の header 中身比較に任せる | (b) は clang の版で predefine が変わる場合を取りこぼしうる。warm `run` が toolchain に触れない今の性質を保つかどうか |
| Q6 | 生成物のチェックイン(§3 案 B)をどこまで支援するか | (a) inspection の印字だけ (b) 「生成物と manifest が一致するか」を `kizu check` が検査するモード | Cargo は diff 検査 test を勧める。(b) は manifest に語彙が増える |
| Q7 | union / bitfield / 固定長配列 field / extern 変数の受け皿を作るか | 個別に判断。作らない間は opaque / skip | 実需のある C library で必要になったものから。どれも `extern "c" struct` の SPEC 変更 |
