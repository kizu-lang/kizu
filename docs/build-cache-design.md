# build の再利用単位を module にする(設計)

`docs/perf.md` の「cache 設計の制約」が掲げる module-aware key と「public interface が
変わらない編集では依存 module を再処理しない」を、どの順でどう実装するかを決める文書です。
目標は Go と同じ単位(1 directory = 1 module = 1 object、依存は export data 越し)。
決まった部分は実装と同時に `docs/perf.md` / `docs/architecture.md` / 該当 ADR へ移し、
この文書は役目を終えたら消します。

## 0. 要約

| 段 | 内容 | 速くなるもの | 前提 |
| --- | --- | --- | --- |
| **A0** | 計測(clang 時間、unit ごとの IR 量) | ― | なし |
| **A1(最初の一段)** | front end は今のまま全体。debug build の LLVM 出力を module ごとの unit に割り、unit ごとに object を cache して link | 1 module の編集で clang が compile し直すのはその unit だけ | perf-audit #19(cache の LRU 化) |
| A2 | unit の compile を並行に起動 | cold build / bootstrap 第 2 段の wall time | A1 |
| B1 | export data と public interface hash を作る(まだ skip には使わない) | ― (`why-rebuild` の説明) | なし |
| B2 | compiler identity を cache key に入れられるようにする | ― | 未決 U1 |
| B3 | module ごとに front end を回し、unit text を cache する | front end(compiler で 7〜10s)の再処理範囲 | B1, B2, 未決 U2 |

A1 は cache key が**生成物の中身(unit の IR text)そのもの**なので、interface hash も
compiler identity も要らず、stale hit が構造的に起きない(ADR-0126 と同じ理屈)。
interface hash が要るのは front end を飛ばす B3 からで、そこが Go の export data に当たる。

## 1. 現状(根拠)

- 実行ファイルの key は LLVM IR 全文 + runtime source hash + toolchain
  (`internal/native/build.go` `Executable` / `executableCacheTarget`)。
  compiler の `.ll` は 59MB / 77 万行の 1 本(findings/perf-audit.md #12、PR #1927)。
- front end は package と std を 1 本の `ast.Program` に合流させる
  (`internal/project/load.go` `program()`)。型・所有権・IR はその全体を 1 回で処理する。
- IR に source 位置は入らない(perf-audit #12)ので、コメントだけの編集は今も hit する。
- 全体に依存しているものが IR / LLVM 出力に 5 つある。割るときはこれを 1 つずつ処分する。

| 全体依存 | 場所 | 処分 |
| --- | --- | --- |
| Kizu 関数は全部 `internal` linkage | `internal/llvm/emit.go` `functionLinkage` | §2.2 |
| `@.str.N` / `@.table.N` の番号が module 全体の通し番号 | `emit.go` `collectStrings` / `collectTables` | unit ごとに振る |
| 型定義・error union / optional 型を module 全体分書く | `emit.go` `writeHeader` | unit が参照する型だけ書く |
| error name table、task / thread thunk、package `main` | `writeErrorNameTable`、`writeTaskInvokeThunks`、`cmd/kizu/load.go` `addPackageMain` | program unit(§2.1) |
| user error set の code を宣言順の通し番号で振る | `internal/ir/lower.go` `lowerErrorSet`(`nextErrorCode`) | A1 は影響なし。B3 で未決 U2 |

compiler/ の対応箇所: `compiler/src/internal/llvm/llvm.kizu`(emit)、
`compiler/src/internal/native/native.kizu`(`executable` / `link` / `executable_cache_target`)、
`compiler/src/internal/buildcache/buildcache.kizu`、`compiler/src/main.kizu`(`addPackageMain` 相当)。

## 2. module ごとの生成物(A1)

### 2.1 unit の割り方

1 unit = 1 module(std の module も 1 unit)+ program unit 1 つ。

- `ir.Function` に `Module string` を足し、lowerer が宣言の module を入れる。
  symbol 名(`pkg__mod__f`)を割って module を推測しない(`typ` が文字列手術を
  やめさせたのと同じ理由)。project loader が decl に module path を持たせ、lowerer が写す。
- 所属の決め方
  - 普通の関数・method・test 関数: 宣言した file の module
  - derived deinit(`ast.AddDerivedDeinits`): その型の module
  - generic instance: §3
  - error name table、thunk、package `main`、test main: **program unit**。全体から
    決まる小さいものをここに集め、他の unit を全体依存から外す
- unit の順序は `orderedModules()` の順(依存が先)、最後に program unit。
  unit 内の関数順は今の `module.Functions` の順。どちらも両実装が既に一致させている順で、
  map の反復順を使わない。
- `KeepTargetReachableFunctions` は割る前に今のまま全体で行う。

### 2.2 linkage と symbol

`functionLinkage` が全部 internal にしているのは、Kizu の `fn send` が runtime の
libc 呼び出しを横取りしないため(同関数の comment)。unit を跨ぐ呼び出しには external な
symbol が要るので、その安全性を **名前で** 保つ。

- split mode では、unit 外から呼ばれうる関数(他 module から見える関数、generic body が
  名指す private 関数)を `external hidden` にし、symbol 名に **C の識別子にも
  `llvmNamePart` の出力にも現れない文字**(例: `-`)を含める。`kz-app__send` のような形。
  綴りは実装で決めてよいが、この 2 条件は崩さない。
- 呼ばれうるかどうかは「その module 自身」から決める。他 unit が実際に呼ぶかで決めると、
  他 unit の編集でこの unit の text が変わる。
- `main` と `export "c"` は今と同じ。single unit(`--opt`)では今と同じ全 internal。

### 2.3 unit text と cache key

- unit text は、その unit の関数、参照する型の定義、呼ぶ外部関数の `declare`、
  unit 内で振った `@.str` / `@.table`、runtime 宣言(compiler 固定の一覧)だけでできる。
  他 module の private 関数の本体や型の追加で text が変わらないこと。
- `moduleForLinker`(Darwin の stack probe、Windows の probe 削除)は unit ごとに当てる。

| artifact | key | 保存条件 |
| --- | --- | --- |
| `native-obj`(新) | unit text + toolchainKey + `clang --version` 1 行目 | debug の `run` / `test` / `build` の link で miss した unit |
| `native-exe` | 全 unit text の hash を unit 順に並べたもの + runtime source hash + toolchainKey | 今と同じ |

- exe の key は今と同じく clang の版を含めない。warm の `run` は今どおり toolchain に
  触れない(`executableCacheTarget` の comment の理屈)。exe が miss したときだけ
  `--version` を聞き、object の key を作る。object は別の clang の object と混ぜて
  link されうるので版を key に入れる(runtime object と同じ理由)。
- link は `clang <unit objects in unit order> <runtime object> -o out`。`--emit obj` は
  同じ並びを `-r` でまとめる。
- `build --target native` は object を cache から読み、link は毎回する。「名前を指定した
  成果物は cache から読まない」という今の理由は最終成果物についてで、中間物には及ばない。
  bootstrap の扱いは §5。
- `docs/perf.md` の表・`cache status` の種別・`just perf-cache` の single-file edit 測定を
  同じ変更で更新する(perf.md の制約)。

## 3. generic instance の所有権

**使う側の unit に置く**。監査の見立ては正しい。根拠:

- ADR-0066 は instance ごとに body を検査・lower する(C++ template / Rust の
  monomorphize と同じ)。instance の code は型引数の layout で決まり、その型は宣言側
  module から見えない(依存の向きが逆)ことがある。宣言側には置けない。
- 1 つの owner unit を全体から選ぶ(最初に要求した module など)と、別 module に使用が
  1 つ増えただけで instance の居場所が動き、無関係な unit が compile し直しになる。

置き方:

- 各 unit は、自分の関数から(instance を辿って)届く instance を全部 `linkonce_odr hidden`
  で持つ。Go の DUPOK symbol と同じ扱い。lowerer の memo(`instantiated`)は今のままで、
  emit 時に unit ごとに閉包を取る。
- 重複排除は linker。ELF / COFF は `comdat any` を付け、Mach-O は comdat が無いので
  weak 定義の合併に任せる。emit が既に受けている target 引数(`EmitNative` の darwin)で
  分ける。
- 代償は同じ instance を使う unit の数だけ compile すること。Go の stenciling より重いが、
  ADR-0066 の単相化を変えない限り残る(§6)。

## 4. `--opt` と ThinLTO

- **A1 では `--opt` は single unit のまま**(今と同じ 1 本の module を -O3)。分割は
  partition の引数 1 つ(debug: module ごと、opt: 1 つ)で、経路は 1 本のまま(原理 9)。
- 理由: `--opt` は binary の速さが要る release 用で、-O3 は今 whole-program に効いている。
  分割すると LLVM の cross-module inline が消える。Kizu IR の `Inline` は分割前の全体に
  かかるので残る。
- ThinLTO を入れるかは **未決 U3**。

## 5. bootstrap のバイト一致

`TestSelfhostBootstrap` は seed が作った stage1 と、stage1 が自分を build した stage2 の
byte 一致を見る。分割後も一致させるため、両実装に同じ規則を置く。

一致を保つ規則:

1. 割り方は data からだけ決める: `ir.Function.Module`、`orderedModules()` の順、
   `module.Functions` の順。map の反復順、file system の列挙順、時刻、並行完了順を使わない。
2. unit 内の番号(`@.str`、`@.table`、attribute group)は unit の関数列だけから振る。
3. link に渡す object の順は unit 順で固定。並行 compile(A2)は完了順でなく unit 順に並べる。
4. `linkonce_odr` は linker が link 順で最初の定義を採る。全 copy は同じ IR text から
   同じ flag で作るので、どれが採られても順が固定なら結果は固定。
5. stage2 は**空の `KIZU_CACHE_DIR` で build する**。共有 cache だと stage1 が作った object に
   hit し、clang の非決定性を見逃す。`docs/perf.md` の「fixed point の検査は cache hit で
   省略しない」をこれで保つ。

崩れる条件(実装時にここを潰す):

| 条件 | 起き方 | 守るテスト |
| --- | --- | --- |
| unit 割りが両実装で違う | Kizu 側で順序付き Array の代わりに map を回した等 | llvm corpus に unit 分割の出力を足す(`compiler/tests/`、`-update` で両方更新) |
| 型定義・declare の書き出し順が違う | 参照集合を集合のまま書く | 同上 |
| clang の版違いの object が混ざる | object key に版が無い | object key の unit test |
| Apple ld の -O0 での配置揺れ | object 数が増えて顕在化しうる | 既存の `-Wl,-reproducible` + bootstrap |
| comdat の扱いが target で違う | Mach-O に comdat を書く | conformance の macOS / Linux 両 CI |
| 並行完了順が link 順に漏れる | A2 | bootstrap |

## 6. public interface hash(B1)

### 6.1 定義

module M の **export data** = M に依存する module の front end が M について読んでよい
情報の全部。interface hash = その canonical な byte 列の SHA-256。

保証の仕組みは Go と同じで、**依存 module は M を export data 越しにしか読めない**ように
する。export data に無いものは依存側の出力に影響できないので、実装だけの変更で hash が
変わらないことと、hash が変わらなければ依存側を再処理しなくてよいことが同じ事実になる。
「何を含めるか」を人が列挙して漏れを探すのではなく、読む側の入口を 1 つにして漏れを
compile error にする。

含めるもの(Go の export data との対応):

| 内容 | Go の export data | 備考 |
| --- | --- | --- |
| pub 関数・method の署名: static 引数、引数の型と borrow / owned、返り値、error set、capability 引数、`unsafe`、`extern` / `export` ABI と `@link_library` / `@link_framework` | exported func の型 | |
| pub 型の完全な宣言: **private field を含む**全 field と順序、union / enum の variant と payload、`unsafe struct`、宣言 / derived の deinit の有無、owner か copy か | 型は unexported field まで入る | layout と owner 性(`AddDerivedDeinits` は field から owner を決める)が private field で変わるため |
| pub 定数の値 | constant の値 | |
| error set の member と code | (該当なし) | code の決め方は未決 U2 |
| generic 関数の body(qualified な正規形) | generic body が入る | instance を使う側で lower するため(§3) |
| generic body が名指す private 宣言の署名・型(generic なら body も) | inline body が参照する unexported object | |
| 上に現れる他 module の型 | 参照先 package の型も自己完結で入る | 依存の依存の hash を畳み込まない(畳み込むと interface 変更が推移閉包全体に波及する) |

含めないもの: 非 generic 関数の body、private 関数(上記の例外を除く)、source 位置、
コメント、import の並び。source 位置は依存側の診断のために hash の外の side table に置く。
依存側の生成物は位置を持たない(IR に位置が入らない、perf-audit #12)ので key に要らない。
例外は test mode の位置(`emitTestMark`)で、これは自 module の位置なので自分の source hash が持つ。

`--opt` の Kizu IR `Inline`(`internal/ir/inline.go`、8 命令以下)は module を跨いで body を
写すので、B3 で `--opt` も module 単位にするなら inline 候補の body を export data に入れる
(Go の inline body と同じ)。A1〜B3 では `--opt` は single unit(§4)なので要らない。

comptime は user 関数を実行しない(ADR-0006)ので、comptime のために body を出す必要はない。

### 6.2 B1 のテスト

- `tests/fixtures/` に編集の対を置き、hash を比べる Go の test を足す: private 関数の body、
  コメント、private 関数の追加では不変 / pub 署名、pub 型の private field、generic body の
  変更では変化。
- 両実装の export data を corpus で byte 比較する(hash の一致を両実装で揃える)。

## 7. 移行手順

### A0: 計測(最初の一段の前提確認)

- `compiler.ll` の clang -O0 compile 時間と、module ごとの IR 量。監査は clang 時間を
  測れていない(perf-audit §0)。source 量で見ると最大の module(`internal/types`)が
  compiler/ 非 test source の約 17% なので、1 module 編集の再 compile は全体の 2 割弱に
  収まる見込み([推測]、A0 で確かめる)。
- 測定は `just perf-cache` の single-file edit に compiler を対象として足す。

### A1(最初の一段): debug の unit 分割と object cache

- 変更: §2 と §3。Go と compiler/ の両方(AGENTS.md)。
- 先に perf-audit #19 を直す。object entry が増えると、FIFO では使っている object が
  消え、miss ごとの全 entry 走査が効いてくる。
- 速くなるもの: `run` / `test` / `build` で 1 module を変えたときの clang 時間。
  `kizu test compiler` と `just selfhost` が主な受益者。front end と emit は今と同じ時間。
- 守るテスト
  - conformance(`go test ./cmd/kizu`): 全 example が分割経路で動く
  - llvm corpus: unit 分割の出力が両実装で byte 一致
  - `TestSelfhostBootstrap`: §5 の規則 5 込み
  - 新規 **incremental = clean**: package を build、1 module を編集して warm cache で
    rebuild した exe と、空 cache で build した exe が byte 一致すること。合わせて
    cache の miss が「編集した unit + program unit(署名や型を変えたならそれを参照する
    unit も)」であることを `cache status` / `why-rebuild` の出力で確かめる(関数の形を grep する
    構造 pin ではない)

### A2: unit の並行 compile

unit 単位の clang を並行に起動する。Go が package を並行に compile するのと同じ単位で、
テストを並べてごまかす類ではない。cold build と bootstrap 第 2 段の wall time が縮む。
規則 3(link 順は unit 順)を守る。

### B1: export data と interface hash

§6。まだ skip には使わず、`why-rebuild` が「M の interface が変わったので N を再処理」を
説明できるところまで。

### B2: compiler identity

front end の出力(unit text)を cache するには、key に compiler 自身が要る。ADR-0126 は
これが無いので text cache を廃止した。未決 U1 を決め、ADR-0126 を書き換える
(新しい ADR は足さない)。

### B3: module ごとの front end

- module を依存順に 1 つずつ型・所有権検査・lower する。依存は export data から読む。
- unit text の key: M の source hash + 直接 import の interface hash + compiler identity +
  target + opt mode + manifest hash(perf.md の key 一覧と同じもの)。
- 速くなるもの: 1 module 編集時の front end と emit。interface が変わらなければ
  依存 module は front end も clang も通らない。
- 守るテスト: A1 のもの全部 + B1 の hash テスト + incremental = clean を front end skip 込みで。
- 片付けるもの: `nextErrorCode`(U2)、test 関数の通し番号(`TestFunctionName(ordinal)` を
  module 内で決める)、std の reachability 駆動 lower(§8)。

## 8. やらないこと・Go との差

| Go と同じにしない点 | 理由 |
| --- | --- |
| generic を shape + dictionary で共有しない | ADR-0066 の単相化を保つ。使う unit ごとの重複 compile を代償として受ける |
| `--opt` は module に割らない(当面) | -O3 の whole-program 最適化を失う。release は incremental より binary の速さ(U3) |
| std を全体 precompile しない(A1〜B3) | 今は到達した std 関数だけを lower し、link 入力も到達した extern から決めている(`setLinkInputs`)。全体を object にすると dead strip と link 入力の要約が要る。B3 後に別途判断 |
| wasm は割らない | wasm は 1 module を出す backend で clang を通らない。到達性で target 非対応を拒否する検査(architecture.md §3)も全体で行う |
| loose file(1 file の program)は得をしない | 1 module なので。std の unit だけが再利用される |

## 9. 未決

| ID | 問い | 選択肢 | 判断材料 |
| --- | --- | --- | --- |
| U1 | compiler identity をどう作るか(B2) | (a) 実行ファイルの path を std から取れるようにし binary を hash する(Go の build ID 相当) (b) build 時に compiler/ と std の source tree hash を version module に生成して埋める(`gen-selfhost-version` の延長、Go seed も同じ規則で自分の source hash を持つ) | ADR-0126 は「selfhost が自分の path を知れない」「version 文字列は開発中に変わらない」で却下した。(b) は両方を避け両実装で対称。(a) は毎回 18MB 級の hash(perf-audit #3 で Kizu の SHA-256 は遅い) |
| U2 | error code を module 間で独立に決める方法(B3) | (a) qualified な member 名の hash から code を作り、program unit で衝突を検査 (b) module ごとの番号範囲 (c) 現状維持し、error set を持つ module の追加・削除で後続 module を再処理する | ADR-0127(合成 set は code の和集合で再番号しない)と、runtime の `#define` は std の set だけを使うこと(`runtimeErrorSets`)。code の値が SPEC の約束に入るなら SPEC 判断 |
| U3 | `--opt` に ThinLTO を入れるか | (a) single unit のまま (b) unit を bitcode にして `-flto=thin` + ThinLTO cache | toolchain 依存(Apple ld64 の libLTO、llvm-mingw の lld)、binary 速度の差、release build 時間。A1 の後に compiler の `--opt` build で測る |
| U4 | debug の `build --emit-llvm` が何を印字するか | (a) 今どおり single unit の text (b) unit ごとの file を directory に出す | (a) は compile される text と別物になる。(b) は出力の形が変わる |
