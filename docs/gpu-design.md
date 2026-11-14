# GPU 向け build(compute)の設計検討

Kizu で GPU に compute kernel を出す場合の形を、判断材料として並べる文書です。
実装はまだありません。範囲は GPGPU(compute)だけで、グラフィックスは扱いません。
決まった部分は実装と同時に `SPEC.md` / `docs/std/` / 該当 ADR へ移し、この文書は
役目を終えたら消します。判断が要る点は末尾の §8 に集めています。

## 0. 要約

| 論点 | 推奨 | 根拠の要点 |
| --- | --- | --- |
| target 形式 | SPIR-V(Vulkan 1.3、Shader capability、GLCompute) | 3 OS・主要 3 ベンダーに届く唯一の形式。driver が機械語を作る |
| backend の置き場所 | LLVM でも direct(arm64)でもなく、wasm と同列の新 backend `spirv` を typed SSA IR から直接書く | wasm backend の構造化制御フロー復元がそのまま要る。外部 toolchain が要らない |
| kernel の入口 | `export "gpu" fn`(`export "c"` / `export "browser"` と同じ枠) | 新しい構文が要らない。到達可能性の root の仕組みが既にある |
| invocation 番号 | **@ builtin ではなく kernel の引数**で受け取る | `@embed_file` は load 時に定数へ畳む仕組みで、invocation ごとに変わる値の先例にならない。引数なら kernel を CPU でそのまま呼べる |
| 引数バッファ | kernel 側は普通の `&[]T`(全員が読む)と `&var []T`(自分の窓だけ書く)。host 側は owner の `gpu::Buffer<T>` | `std::thread::each` と同じ「重ならない窓」の規則で data race を型で閉じる |
| host API | `std::gpu` を Kizu source で書き、`extern "c" fn` で Vulkan を呼ぶ | `docs/stdlib.md` の境界規則(Kizu で書けるものは primitive にしない)に従う |
| 第一歩 | `out[i] = a[i] + b[i]`(u32)を lavapipe と実 GPU で走らせ、CPU で同じ関数を呼んだ結果と一致 | §7 |

## 1. 現行コードベースの制約(根拠)

### 1.1 `export "c"` / `export "browser"`

- 構文は `extern "<abi>" fn` / `export "<abi>" fn` の ABI 文字列 1 つ。検査は
  `compiler/src/internal/types/type_collection.kizu` の
  `validate_host_function_signature` が `"c"` と `"browser"` 以外を
  `UnsupportedExternAbi` / `UnsupportedExportAbi` で拒否している。**新しい ABI 名を
  足す場所はここ 1 箇所で、構文は増えない。**
- export は target の到達可能性 root(SPEC §12.3、§13.3)。`main.kizu` の
  `keep_target_program(allocator, &var module, export_abi)` が target ごとに root に
  する export ABI を 1 つ選ぶ。browser build は `"browser"` を渡している。
- `export "c"` の本体は通常の Kizu 関数で、**Kizu からも普通の関数として呼べる**
  (SPEC §12.3)。kernel を CPU で呼べるようにする案(§3.2)はこの性質をそのまま使う。
- 他 target の export は build 時に拒否する(native は `export "browser"` を、
  wasm は `export "c"` を拒否)。GPU では host build が `export "gpu"` をどう扱うかを
  決める必要がある(§8 D4)。
- ABI 境界を通る型の表は ABI ごとに SPEC が持つ(§12 の C、§12.1 の browser)。
  GPU にも同じ形の表が 1 つ要る。

### 1.2 @ builtin

- SPEC §6.13: 「builtin は `@embed_file` だけです」。parser
  (`parser.kizu` の `parse_builtin_expr`)が名前を 1 つだけ受け、**`@embed_file` は
  `ast::StringExpr` の `embed` 付きとして parse され、loader が file の bytes を入れて
  以後は文字列 literal として扱われる。** 型検査・IR・backend は builtin の存在を
  知らない。
- 宣言に付く attribute は別系統で、`@link_library` / `@link_framework` が
  `extern "c" fn` にだけ付く(SPEC §12.2)。
- したがって「`@embed_file` と同じ系統で thread 番号を返す」は成り立たない。
  `@embed_file` は compile 時に定数になる値で、invocation ごとに違う runtime 値を
  返す builtin は今の compiler に枠が無い(IR に新しい Op、型検査に文脈規則、
  全 backend に「kernel 外では compile error」の分岐が要る)。§3.2 で比較する。

### 1.3 C 境界

- `extern "c" fn` は C が名指しできる型だけを通す(整数・浮動小数・`bool`・raw
  pointer・C 関数 pointer、`extern "c" struct` の borrow と値渡し)。呼び出しには
  `unsafe` と `// SAFETY:` が要る(SPEC §12、§12.2、§12.4)。
- `extern "c" struct` で C layout を明示できる。Vulkan の `Vk*CreateInfo` 群
  (`sType` / `pNext` / pointer field)はこれで書ける。`vkGetInstanceProcAddr` が返す
  関数 pointer は `?extern "c" fn(...)` で受けられる。
- `@link_library("vulkan")` で link、探索 directory は manifest の `[native]` が持つ。
  呼ばれない宣言の library は link しない(reachability で閉じる)。
- `kizu import-c-header` は experimental で可変長・struct 中心の header は変換しない。
  `vulkan.h` は手で必要分だけ宣言することになる。
- **現状 `lib/kizu/std/src` に `extern "c" fn` は 1 つも無い。** OS 境界は全部
  `std::internal::builtin::*` の trusted primitive(compiler runtime の C)経由。
  `std::gpu` が `extern "c"` で書かれれば、std が外部 library を link する最初の
  module になる(§5.3、§8 D8)。

### 1.4 compiler の経路

- native は target ごとに backend 1 つ: darwin-arm64 / linux-amd64 / windows-amd64 は
  direct、他は LLVM(SPEC §5)。wasm は typed SSA IR から 1 度だけ WebAssembly module
  へ lower し、WAT と binary に描画する(ADR-0022)。
- LLVM 経路は runtime の C(`kizu_` symbol)を clang で compile して link する前提で、
  IR は host の pointer を自由に使う。direct 経路は arm64 / amd64 の機械語エンコーダ。
  **どちらも GPU には乗らない**: SPIR-V は runtime を link できず、pointer の扱いが違う。
- wasm backend(`compiler/src/internal/wasm/structure.kizu`、`control.kizu`)は SSA の
  CFG から loop header と merge 点を求め、構造化制御フローを書いている。SPIR-V の
  Shader 系は `OpSelectionMerge` / `OpLoopMerge` を要求する構造化制御フローなので、
  この解析を共有できる。
- devShell の LLVM は 21.1.8 で、SPIR-V target を持っている(手元で確認):

  ```text
  $ clang -print-targets | grep -i spir
      spirv       - SPIR-V Logical
      spirv32     - SPIR-V 32-bit
      spirv64     - SPIR-V 64-bit
  ```

  最小の `.ll` を unwrapped clang で compile して header を読んだ結果:

  | triple | capability | addressing / memory | execution model |
  | --- | --- | --- | --- |
  | `spirv64-unknown-unknown` | Kernel, Addresses | Physical64 / OpenCL | Kernel(**Vulkan は読めない**) |
  | `spirv-unknown-vulkan1.3-compute` | Shader | Logical / GLSL450 | GLCompute |

  Vulkan 向けの出力は可能だが、LLVM の Vulkan 経路は HLSL frontend 用の resource
  intrinsic(`llvm.spv.resource.*`、`target("spirv.VulkanBuffer", ...)`)で buffer を
  表す設計で、まだ動いている。nix の cc-wrapper は host 用 flag(`-fzero-call-used-regs`)
  を足すので、使うなら wrapper を外す手当ても要る。
- `spirv-val` / `spirv-as`(SPIRV-Tools)は devShell に無い。

### 1.5 kernel に効く既存の規則

- borrow は struct field に保存できず、block の外へ escape しない(SPEC §0.1、§4)。
  view を持つ struct も tie を運ぶだけで、heap や global に逃げない。
  **このため kernel 内の view はすべて静的に引数まで辿れる。** SPIR-V Logical
  addressing(pointer は常に静的な場所に解決できなければならない)と相性が良い。
- 書き換えられる global は無い(SPEC §15.3)。kernel が触れるのは引数だけになる。
- 確保は明示 allocator(原理 4)。kernel が `Allocator` を引数に取れなければ、kernel
  内の heap 確保は型の段階で起きない。
- 添字の範囲検査は IR が持つ(ADR-0084)。panic の出口を GPU 上で決める必要がある(§3.4)。
- `std::thread::each` は slice を重ならない塊に切って worker に `&var []T` で渡し、
  data race を型で閉じている(SPEC §15.3)。GPU の dispatch はこれの多スレッド版として
  説明できる(§4)。

## 2. target 形式の選定

| | SPIR-V(Vulkan) | PTX(CUDA) | Metal(MSL / metallib) |
| --- | --- | --- | --- |
| OS | Windows / Linux / Android。macOS・iOS は MoltenVK 経由 | Windows / Linux | macOS / iOS のみ |
| ベンダー | NVIDIA / AMD / Intel / ARM / Qualcomm、Apple は MoltenVK | NVIDIA のみ | Apple のみ |
| driver | GPU driver が Vulkan loader と ICD を持つ。macOS は Apple が配らず、利用者が Vulkan SDK / MoltenVK を入れる | NVIDIA driver に PTX JIT が入っている | OS 同梱 |
| ツールチェーン負担 | binary は word 列で仕様公開、直接書ける。検証器 `spirv-val` は OSS | LLVM NVPTX で出せる。text 形式 | MSL は C++ 方言の text。metallib は Xcode の閉じた `metal` tool が要る |
| 既存 backend との関係 | wasm と同じく構造化制御フローの binary 形式。直接書く backend にできる | LLVM backend の別 triple になるが、runtime 前提を外す作業は同じ量 | text を生成する backend になり、「生成 text を組み立てない」方針(原理 11)と相性が悪い |
| 外部の CI | lavapipe(Mesa の CPU 実装)で GPU 無しに実行できる | GPU 必須 | macOS runner の GPU 次第 |

**推奨: SPIR-V(Vulkan 1.3、Shader capability、GLCompute execution model)。**

- 1 つの形式で 3 OS と主要ベンダーに届くのは SPIR-V だけ。原理 9(target ごとに
  経路 1 本)を守るなら、最初の 1 本は到達範囲の最も広いものにする。
- binary が仕様で決まり、検証器と CPU 実装(lavapipe)が OSS にある。検証を CI に
  乗せられる(§6)。
- macOS は MoltenVK の SPIR-V→MSL 変換を挟むので、第一級ではなく「届く」扱いになる。
  これは source に見せる制約として `docs/std/gpu.md` に書く。
- OpenCL 系の SPIR-V(Kernel capability)は Vulkan が読めないので選ばない。

## 3. kernel の表現

### 3.1 入口の指定

| 案 | 形 | SPEC への影響 | 原理との関係 |
| --- | --- | --- | --- |
| **A. export 方式(推奨)** | `export "gpu" fn add(...) -> void { ... }` | §12 に GPU ABI の節を 1 つ。ABI 文字列を 1 つ足す | 「外から呼ばれる口」を既存の枠で言う。構文が増えない(原理 6、10) |
| B. 属性方式(rust-gpu) | `@compute(64) fn add(...)` | 式でない attribute を `extern "c"` 以外の関数にも開く | workgroup size を source に書ける。ただし attribute 系統が `extern` 専用から一般化する |
| C. callconv(Zig) | `fn add(...) callconv(.kernel)` | 関数型に呼び出し規約の構文を足す | 呼び出し規約は関数間 call の話で、入口とは別。Kizu には C 関数 pointer 型の `extern "c" fn(...)` しか規約の綴りが無い |
| D. 言語サブセットを分ける | `kernel fn` や別拡張子 | 新しい keyword、型検査の別系統 | 書き方が 2 通りになる(原理 9、§2 非目標「何通りもの書き方」) |

ABI 名は `"spirv"` より **`"gpu"`** を推す(§8 D2)。`export "browser"` は形式(wasm)
でなく呼ぶ側の host を名乗っている。kernel の契約(引数の並びと窓の規則)は SPIR-V に
依らず、将来 Metal backend を足しても source は同じでよい。形式は `--target` が選ぶ。

### 3.2 invocation 番号の受け取り方

| 案 | 形 | 評価 |
| --- | --- | --- |
| I. @ builtin | `let i = @global_id(0);` | invocation ごとに違う runtime 値を返す builtin は今の compiler に無い(§1.2)。kernel 外での使用を compile error にする文脈規則が要る。kernel を CPU で呼べない |
| **II. 引数(推奨)** | `export "gpu" fn add(index: u32, ...)` | 構文が要らない。backend が第 1 引数を `BuiltIn GlobalInvocationId` の x に写す。**kernel は CPU から普通の関数として呼べる**ので、CPU 参照実装を別に書かなくてよい(§6.2) |
| III. compiler 定義の std 関数 | `gpu::global_id()` | 見た目は関数呼び出しだが、値が呼び出し元の文脈で変わる隠れた入力になる |

II の詳細:

- 第 1 引数は `index: u32`(1 次元)。2・3 次元は第 1 引数の型を `gpu::Index2` /
  `gpu::Index3`(`x` / `y` field の copy struct)にする形で後から足せる(閉→開、原理 8)。
- workgroup 内番号・workgroup 番号は shared memory を入れる段階(§7 G5)まで持たない。
  その時も引数で足す。

### 3.3 kernel 内で使える言語機能

制限は新しい構文で言わず、wasm target と同じく「`export "gpu"` から到達する関数に
target が扱えない操作があれば build 時に拒否する」形にする(`spirv error:` で止まり、
他の backend へは切り替えない)。拒否の多くは型が先に閉じる。

| 機能 | 扱い | 理由 |
| --- | --- | --- |
| 整数 `i32` / `u32` / `bool`、`f32` | 使える | Vulkan core |
| `i64` / `u64` / `usize` | 使える(`Int64` を device 要件にする) | slice の長さと添字が 64 bit のため。外すと slice の表現が変わる |
| `f64` | 第一歩では拒否 | `Float64` は optional で、Apple GPU には無い |
| `i8` / `u8` / `i16` / `u16` の storage | 第一歩では拒否 | `8/16bit storage` は optional |
| vector 型 `f32x4` 等 | 使える(G4) | `OpTypeVector` にそのまま写る |
| struct / 固定長配列 / local `var` / `if` / `while` / `for` / `match` | 使える | Function storage の値 |
| 関数呼び出し | 使える。view を渡す呼び出しは backend が inline する | Logical addressing は StorageBuffer の pointer を関数引数にできない(VariablePointers 無しの場合) |
| 再帰 | 拒否 | SPIR-V が禁止。呼び出し graph の閉路で検出 |
| 関数 pointer(`fn(...)` 値) | 拒否 | SPIR-V に間接呼び出しが無い |
| `Allocator` / `Io` / owner(`String` `Array` `Map` `Box` `Arena`) | 拒否(引数に取れない) | 確保は明示 allocator なので、kernel が allocator を受けられなければ確保は起きない |
| 文字列 literal / `print` | 拒否 | 出力先が無い。byte storage も optional |
| `extern "c"` / `std::thread` / `std::coro` | 拒否 | host の capability |
| `defer` / `errdefer` | 使える | 呼び出しが source にあり、制御は構造化される |
| error union を返す | kernel 自体は `void` のみ。内部関数の `!T` は G4 で検討 | 失敗を host へ運ぶ ABI が要るため、まずは panic 経路だけ(§3.4) |
| raw pointer / `unsafe` | 第一歩では拒否 | Logical addressing に任意 address が無い |

### 3.4 panic(範囲検査)の出口

GPU には trap も process 停止も無い(`OpKill` は fragment 専用)。案:

| 案 | 評価 |
| --- | --- |
| **a. status buffer に記録して invocation を return(推奨)** | target ABI が 1 つ storage buffer を予約し、panic した invocation が atomic に印を付けて抜ける。host の `run` は `gpu::Error::KernelPanicked` を返す。wasm の `kizu_start` が status を返すのと同じく、target ABI として文書化する(隠さない) |
| b. panic 経路のある kernel を拒否 | 範囲検査はほぼ全ての添字に入るので書けるものが無くなる |
| c. 範囲検査を GPU では外す | 原理 1 に反する |

他の invocation は走り続け、出力は部分的に書かれている。`run` が error を返したら
出力 buffer の中身を信じない、が契約になる。

## 4. メモリモデルと所有権

### 4.1 型の区別

| 置き場所 | 型 | 所有 |
| --- | --- | --- |
| host のメモリ | `[]T` / `std::array::Array<T>`(今のまま) | 今の規則 |
| device のメモリ(host 側から見た名前) | `std::gpu::Buffer<T>` | owner。`deinit(&var device)` を `defer` で書く。host から添字で読めない |
| kernel の引数 | `&[]T`(読む)、`&var []T`(書く) | 普通の borrow。kernel は Buffer 型を知らない |

- host slice と device buffer は別の型で、暗黙の変換は無い。転送は
  `upload` / `download` の明示 call(原理 2)。「同じ `[]T` が host にも device にもある」
  ように見せる unified memory は採らない(§8 D6)。
- **address space の構文は足さない。** kernel 内の view は全部引数から来る(§1.5)ので、
  backend が由来から storage class を決める: 引数由来は StorageBuffer、local は
  Function。shared memory を入れる段階で初めて、それを表す型(`gpu::Shared<[N]T>` 等)を
  考える。

### 4.2 書き込みの窓

kernel の `&var []T` は buffer 全体ではなく、**その invocation に割り当てた重ならない
窓**を指す。dispatch は `width` 個ずつ切り、invocation `index` に
`[index * width, (index + 1) * width)` を渡す。`std::thread::each` の `chunk` と同じ規則で、
書き込みが重ならないので data race は書けない。`&[]T` は buffer 全体を全員が読む。

```kizu
/// a と b の index 番目を足して out に書く。
export "gpu" fn add(index: u32, out: &var []u32, a: &[]u32, b: &[]u32) -> void {
    let i = cast<i64>(index);
    out[0] = a[i] + b[i];
    return;
}
```

CPU で同じ関数を呼ぶときは、呼ぶ側が窓を切って渡す。意味は変わらない:

```kizu
let cells = expected.as_mut_slice();
for 0..count |i| {
    add(cast<u32>(i), &var cells[i..i + 1], input_a, input_b);
}
```

同じ buffer を 1 回の dispatch で `&` と `&var` の両方に渡すことは、host 側の call で
同じ値の `&` と `&var` が重なるので今の borrow 検査が拒否する。

窓で書けないもの(scatter、reduction、histogram)は atomic か shared memory が要る。
これは第一歩に入れず、入れる時は `unsafe` か、`each_with` の state に当たる構造化 API の
どちらかで出す(§8 D5)。

### 4.3 引数の渡し方

| 案 | 中身 | 評価 |
| --- | --- | --- |
| **P1. descriptor に束ねた storage buffer(Logical addressing、推奨)** | 引数ごとに binding を 1 つ。backend が view を (binding, offset, len) として追う | Vulkan 1.0 からの基本形で driver 差が最も小さい。`robustBufferAccess` が効く。Kizu の borrow 規則で view が静的に辿れるので成り立つ。view を渡す呼び出しは inline が要る |
| P2. buffer device address を push constant で渡す(PhysicalStorageBuffer64) | slice を (u64 address, len) のまま渡す。descriptor set が要らない | Kizu の slice 表現がそのまま残り backend が単純。代わりに OOB が本物のメモリ破壊になり、MoltenVK・古い driver の対応を個別に確かめる必要がある |
| P3. 型付き Buffer handle を kernel に見せる | kernel 引数を `gpu::Buffer<T>` にし、method で読み書き | kernel が CPU で呼べなくなる。書き込みの重なりを型で閉じられない |

push constant は scalar 引数(`n: u32` 等)に使う。scalar は全 invocation に同じ値が
copy されるので by-value の規則と一致する。

### 4.4 所有と借用の GPU 版

- `run` は**同期**で、kernel が終わってから返る。kernel に貸した buffer の borrow は
  call の間だけで、`std::thread::each` と同じく普通の call の借用規則で足りる。
- 非同期 dispatch(返り値を後で待つ)は `std::io::Future` と同じく、貸した buffer に
  tie した値として後段で設計する(§7 G5)。in-flight の buffer に host が触れないことを
  borrow で閉じる。

## 5. host API(`std::gpu`)のスケッチ

### 5.1 形

```kizu
import std::gpu;

fn sum(io: Io, allocator: Allocator, a: []u32, b: []u32, out: &var []u32) -> !void {
    var device = try gpu::open(io, allocator);
    defer device.deinit(allocator);

    var program = try device.load(io, allocator, @embed_file("build/kernels.spv"));
    defer program.deinit(&var device);

    var buffer_a = try device.buffer<u32>(io, mem::count<u32>(a));
    defer buffer_a.deinit(&var device);
    var buffer_b = try device.buffer<u32>(io, mem::count<u32>(b));
    defer buffer_b.deinit(&var device);
    var buffer_out = try device.buffer<u32>(io, mem::count<u32>(a));
    defer buffer_out.deinit(&var device);

    try device.upload<u32>(io, &var buffer_a, a);
    try device.upload<u32>(io, &var buffer_b, b);

    var args = gpu::args(allocator);
    defer args.deinit(allocator);
    try args.write<u32>(allocator, &var buffer_out, 1);   // width = 1
    try args.read<u32>(allocator, &buffer_a);
    try args.read<u32>(allocator, &buffer_b);
    try device.run(io, &program, "add", mem::count<u32>(a), &var args);

    try device.download<u32>(io, &buffer_out, out);
    return;
}
```

| 操作 | 署名の要点 |
| --- | --- |
| デバイス取得 | `gpu::open(io, allocator) -> gpu::Error!gpu::Device`。`Io` は driver に触る権限、`Allocator` は host 側の管理構造 |
| buffer 確保 | `device.buffer<T>(io, count) -> gpu::Error!gpu::Buffer<T>`。device メモリは host の allocator から取らないので `Allocator` は取らず、失敗は `OutOfDeviceMemory` |
| 転送 | `upload<T>(io, &var Buffer<T>, []T)` / `download<T>(io, &Buffer<T>, &var []T)`。長さ不一致は `LengthMismatch` |
| 起動と同期 | `run(io, &program, name, invocations, &var args)` が完了まで待つ |
| 解放 | すべて owner の `deinit`。`defer` で書く |

`gpu::Error` は意味で分ける(原理 7): `NoDevice`、`MissingFeature`、`OutOfDeviceMemory`、
`OutOfMemory`(host)、`DeviceLost`、`KernelPanicked`、`LengthMismatch`、
`SignatureMismatch`。

### 5.2 引数と kernel 署名の照合

上のスケッチは kernel 名を文字列で引き、引数の並びを実行時に照合する。backend は
SPIR-V に加えて kernel ごとの引数表(型・`&` / `&var`・scalar)を module に埋め、
`run` が `args` と照合して食い違えば `SignatureMismatch` を返す。compile 時に照合する
には、host build が kernel の宣言を見られる必要がある(§8 D3、D4)。

### 5.3 既存 std の流儀との整合

| 観点 | `std::net` / `std::http` | `std::gpu` 案 |
| --- | --- | --- |
| capability | 外部に触る関数は `Io` を取る | 同じ。`open` / 転送 / `run` が `Io` を取る |
| 資源 | descriptor を private field に持つ owner、`deinit` | `Device` / `Buffer<T>` / `Program` を owner にし、Vulkan handle は private field |
| 失敗 | `!T`、意味ごとの error | 同じ |
| target 非対応 | 到達する program を build 時に拒否 | 同じ。wasm は `std::gpu` に到達すれば拒否 |
| 実装 | trusted primitive(`net_*`)の上の Kizu | **`extern "c"` で Vulkan を直接呼ぶ Kizu**。std が初めて外部 library を link する |

C 境界の取り方は 2 通りある(§8 D8):

- `@link_library("vulkan")` で link する。宣言は素直だが、Vulkan が入っていない machine
  では実行ファイルが起動しない(macOS は `libvulkan` の探索 directory を利用者の
  manifest `[native]` に書く)。
- loader を `dlopen` で開き `vkGetInstanceProcAddr` から関数 pointer を引く。Vulkan が
  無い machine でも `gpu::open` が `NoDevice` を返すだけで済む。`dlopen` 自体の宣言が要る。

## 6. 検証戦略

### 6.1 SPIR-V の妥当性

- devShell に `spirv-tools` を足し、backend が書いた全 `.spv` を `spirv-val
  --target-env vulkan1.3` に通す。`just wasi-smoke` と同列の `just gpu-smoke` に置き、
  `just selfhost` には入れない(30s 目標と、GPU 無し環境を守るため)。
- `spirv-val` は compiler の外の検査器で、compiler 内部の形を pin しない(構造 pin を
  足さない規則と両立する)。

### 6.2 CPU 参照との差分

- kernel は普通の Kizu 関数なので、同じ example が CPU で窓を切って呼んだ結果と、
  GPU で `run` した結果を両方作って比べられる。`scripts/difftest-direct.sh` と同じ形の
  `scripts/difftest-gpu.sh` を置き、比較対象は example の出力にする。
- 整数 kernel は完全一致を要求する。
- 浮動小数は一致しない場合がある。Vulkan は割り算・超越関数に ULP 誤差を許し、
  denormal の扱いと演算の縮約(FMA 化)が実装に任される。SPEC の浮動小数の意味を
  GPU target で緩めるか(§8 D7)を決め、緩めるなら差分検査は ULP 許容で比べる。
  `NoContraction` 装飾で縮約は止められるが、割り算の精度は止められない。

### 6.3 ベンダー・driver 差

- `gpu::open` が device の version と必要 feature(Vulkan 1.3、`shaderInt64` 等)を問い、
  欠けていれば `MissingFeature` で止める。足りない機能を emulation しない。
- CI は Linux の lavapipe(Mesa の CPU 実装 Vulkan)で回す。GitHub の runner に GPU は
  無い。必須にしない別 job にする(direct の差分 job と同じ扱い)。
- 実 GPU(NVIDIA / AMD / Intel / Apple+MoltenVK)は手元で回し、結果を PR に書く。

### 6.4 Quint / Lean が効く所

- kernel の中身は Quint で見る対象ではない(CPU 参照との差分が持つ)。
- host 側の状態機械 —— buffer の確保 → upload → in-flight → 読める、`DeviceLost` からの
  回復 —— は、**非同期 dispatch を入れる段階**で Quint の対象になる。同期 `run` だけの
  間は状態が call の中で閉じるので、模型の価値が低い。
- 「窓が重ならない」の性質(`index * width` の区間が互いに素で buffer に収まる)は
  Lean で短く書ける。`std::thread::each` と共通の補題にできる。

## 7. 段階計画

規模は wasm backend(`compiler/src/internal/wasm` 約 20k 行、runtime 込み)を物差しにした
目安です。

| 段 | 内容 | 完了の判定 | 規模 |
| --- | --- | --- | --- |
| G0 | `spirv-tools` と lavapipe を devShell に入れ、手書きの最小 SPIR-V を host 側の小さい Kizu program(`extern "c"` で Vulkan)から走らせる | lavapipe と手元の GPU で結果が読める | 小(数百行、std に入れない検証用) |
| **G1(第一歩)** | `export "gpu"`、`--target gpu`(名前は D1)、`spirv` backend の最小形(`u32` / `i64` / `bool`、`if` / `while` / `for`、`&[]T` / `&var []T` 引数、範囲検査 → status buffer)、`std::gpu` の open / buffer / upload / download / run | `examples/gpu/add.kizu` が `out[i] = a[i] + b[i]` を GPU で計算し、同じ関数を CPU で呼んだ結果と一致し、`spirv-val` を通る | 中(backend 3〜5k 行、std::gpu 1.5〜3k 行 —— Vulkan の初期化が大半) |
| G2 | 検証の常設: `just gpu-smoke`、`difftest-gpu`、lavapipe の CI job | 全 gpu example が smoke と差分を通る | 小 |
| G3 | `f32`、vector 型、struct、関数呼び出しの inline、`match`、push constant の scalar 引数 | 行列積(素朴版)、画像の 1 pixel 処理が書ける | 中(2〜4k 行) |
| G4 | 2・3 次元 index、kernel 内の `!T`、compile 時の署名照合(D3) | 2 次元 stencil が書ける | 中 |
| G5 | shared memory と barrier、atomic、非同期 dispatch と Quint 模型、mapped memory | reduction と scatter が書ける | 大。各項目が別の設計判断 |

G1 の時点で SPEC に足すのは §12 の GPU ABI 節(型の表、窓の規則、status buffer)と
§13.3 の target 述語 1 つ(`std::target::is_gpu()`)。`docs/std/gpu.md` を新設する。

## 8. 決めること

| # | 論点 | 選択肢 | 推奨 | 採用時の影響(範囲 / コスト感) |
| --- | --- | --- | --- | --- |
| **D1** | target 形式と backend の置き場所 | (a) SPIR-V を新 backend で直接書く (b) LLVM の `spirv-unknown-vulkan1.3-compute` に乗る (c) PTX (d) Metal | **(a)** | (a) `compiler/src/internal/spirv/` 新設、wasm の構造解析を共有。外部 toolchain 無し。(b) LLVM backend に GPU 用の別 lowering と HLSL 系 resource intrinsic が要り、clang の wrapper も外す。runtime 前提を外す量は (a) と変わらず、動いている API に依存する |
| **D2** | 入口の綴り | (a) `export "gpu"` (b) `export "spirv"` (c) `export "vulkan"` (d) 属性 `@compute` | **(a)** | type_collection の ABI 名を 1 つ足すだけ。(b)(c) は将来 Metal を足すとき source を書き換えさせる |
| **D3** | invocation 番号 | (a) kernel の第 1 引数 (b) `@global_id()` builtin (c) compiler 定義の std 関数 | **(a)** | 構文変更なし、kernel を CPU で呼べる。(b) は parser・型検査・IR・全 backend に「kernel 文脈」の規則が要り、`@embed_file`(load 時定数)とは別物 |
| **D4** | host build と kernel の結び方 | (a) 別 build:`kizu build --target gpu` で `.spv` を作り host が `@embed_file` (b) 1 回の native build が `export "gpu"` を spirv backend にも通し、`std::gpu` が compiler 定義の form で module と署名を引く | **G1 は (a)、G4 で (b) を再判断** | (a) 言語変更なし。ただし build script を持たない(SPEC §2)ので 2 コマンドになり、`.spv` が古くなりうる。署名照合は実行時。(b) 署名を compile 時に照合できる。1 build に 2 backend が走る初めての形で、cache key と到達可能性が target 2 つ分になる。native build で `export "gpu"` を拒否せず普通の関数として扱う規則も要る |
| **D5** | 書き込みの安全規則 | (a) 窓(重ならない `&var []T`)だけ。scatter は後で (b) 最初から raw pointer + `unsafe` を許す (c) 書き込みを全部 `unsafe` | **(a)** | data race を型で閉じる(SPEC §15 の方針を GPU でも守る)。書けない algorithm は G5 まで待つ |
| **D6** | host と device のメモリ | (a) 別型 `gpu::Buffer<T>` と明示転送 (b) unified / mapped memory を `[]T` に見せる | **(a)** | 転送が source に見える(原理 2)。(b) は転送と同期が隠れる |
| **D7** | GPU 上の浮動小数の意味 | (a) SPEC に「gpu target の `f32` は Vulkan の精度要件」と書いて緩める (b) CPU と同じ意味を守り、満たせない演算を拒否 | **(a)** | 精度差を SPEC に見せる(原理 1)。差分検査は ULP 許容。(b) は割り算すら書けなくなる |
| **D8** | Vulkan の取り込み方 | (a) `@link_library("vulkan")` (b) `dlopen` + `vkGetInstanceProcAddr` (c) trusted primitive(runtime の C) | **(b)** | (b) Vulkan が無い machine でも起動し `NoDevice` を返せる。`dlopen` / `dlsym` の宣言と、関数 pointer 表の初期化コードが std に入る。(a) は起動失敗になる。(c) は `docs/stdlib.md` の「Kizu で書けるものは primitive にしない」に反する |
| D9 | 引数の渡し方 | (a) descriptor + Logical addressing (b) buffer device address + push constant | **(a)** | (a) driver 差が小さく `robustBufferAccess` が効く。backend は view を (binding, offset, len) で追い、view を渡す call を inline する。(b) backend は単純だが OOB が本物の破壊になる |
| D10 | panic の出口 | (a) status buffer に印を付けて return (b) panic 経路のある kernel を拒否 | **(a)** | target ABI に予約 binding が 1 つ増え、SPEC の GPU ABI 節に書く |
| D11 | device 要件 | (a) Vulkan 1.3 + `shaderInt64` (b) Vulkan 1.1 から | **(a)** | `LocalSizeId` 等 1.3 core を使える。Int64 は slice 表現のため必須。古い mobile GPU は外れる |
| D12 | workgroup size | (a) specialization constant にし host の `run` が選ぶ (b) source に書く(属性) (c) compiler 固定 | **(a)** | source の構文が要らない。`LocalSizeId` が Vulkan 1.3 core なので D11 とつながる |

最重要は D1(backend の置き場所 —— 実装量の大半を決める)、D3(番号を引数で受ける —— 
`@` builtin 案を採らない理由と CPU 参照の成立)、D4(host build との結び方 —— build script
を持たない言語で 2 つの成果物をどう結ぶか)。

## 参考

- Zig: SPIR-V backend を compiler 本体に持ち、入口を calling convention
  (`callconv(.kernel)` 等)で指定する。Logical addressing が汎用言語に課す制約の議論:
  https://github.com/ziglang/zig/issues/15230
- rust-gpu: 属性(`#[spirv(compute(threads(64)))]`)で入口を、引数の属性で built-in と
  binding を指定する:
  https://github.com/Rust-GPU/rust-gpu
- Mojo: `DeviceContext` が `enqueue_copy`(明示転送)と `enqueue_function`(起動)を持ち、
  kernel 内は `thread_idx` / `block_idx` で番号を読む:
  https://docs.modular.com/mojo/manual/gpu/fundamentals
- LLVM SPIR-V backend(Vulkan 向け triple と Logical addressing):
  https://llvm.org/docs/SPIRVUsage.html
