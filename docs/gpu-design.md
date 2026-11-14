# GPU 向け build(compute)の設計検討

Kizu で GPU に compute kernel を出す場合の形を、判断材料として並べる文書です。
実装はまだありません。範囲は GPGPU(compute)だけで、グラフィックスは扱いません。
決まった部分は実装と同時に `SPEC.md` / `docs/std/` / 該当 ADR へ移し、この文書は
役目を終えたら消します。判断が要る点は末尾の §8 に集めています。

**想定用途の中心は LLM 推論を代表とする機械学習と GPGPU です。** 物差しには
llama.cpp の Vulkan backend(ggml-vulkan)を置き、それが実際に要求している device
機能とカーネルの形を一次資料で押さえて(§9)、言語機能・device 要件・段階計画を
そこから引いています。汎用 compute は同じ道具で書けるものとして付いてきます。

## 0. 要約

| 論点 | 推奨 | 根拠の要点 |
| --- | --- | --- |
| 想定用途 | LLM 推論(llama.cpp 級)を代表とする ML / GPGPU | 量子化重みの行列ベクトル積・行列積、RMSNorm・softmax の reduction、RoPE、attention が書けて速いことを各判断の物差しにする(§9) |
| target 形式 | SPIR-V(Vulkan 1.3、Shader capability、GLCompute) | 3 OS・主要ベンダーに届く唯一の形式。ggml-vulkan が同じ経路で NVIDIA / AMD / Intel / Apple(MoltenVK)の LLM 推論を実際に動かしている |
| backend の置き場所 | LLVM でも direct(arm64)でもなく、wasm と同列の新 backend `spirv` を typed SSA IR から直接書く | wasm backend の構造化制御フロー復元がそのまま要る。外部 toolchain が要らない |
| kernel の入口 | `export "gpu" fn`(`export "c"` / `export "browser"` と同じ枠) | 新しい構文が要らない。到達可能性の root の仕組みが既にある |
| invocation 番号 | **@ builtin ではなく kernel の引数**で受け取る | `@embed_file` は load 時に定数へ畳む仕組みで、invocation ごとに変わる値の先例にならない。引数なら kernel を CPU でそのまま呼べる |
| 引数バッファ | kernel 側は普通の `&[]T`(全員が読む)と `&var []T`(自分の窓だけ書く)。窓は連続(`each`)と飛び飛び(`each_lane`)の 2 種。host 側は owner の `gpu::Buffer<T>` | `std::thread::each` / `each_lane` と同じ「重ならない窓」の規則で data race を型で閉じる。lane 窓が GPU の coalesced access そのもの |
| workgroup の協調 | shared memory と barrier を生で見せず、`std::thread::each_lane_with` と同じ形の**構造化 collective**(workgroup = pool、lane = worker)で出す | reduction と tiled 行列積が data race と barrier の発散を書けない形で書ける。barrier は collective call の終わりにあり source に見える(§3.5、D14) |
| 数値型 | f32 を第一歩から。`u8` / `i8` / `u16` / `i16` の storage を使える。f16 は当面 `u16` の bit 列と Kizu で書いた変換関数、sub-byte(4bit 量子化)は型を足さず pack | 量子化 block(`{d: f16, qs: [16]u8}`)をそのまま struct で読める。新しい型は cooperative matrix まで足さない(§3.3、D13) |
| device 要件 | Vulkan 1.3 + `shaderInt64` + 8 / 16bit storage + `shaderInt8` / `shaderInt16` を必須。cooperative matrix は任意で、host が明示的に分岐 | 必須にしても MoltenVK・lavapipe・desktop 3 社は落ちない。cooperative matrix は MoltenVK に無い(§8 D11、§9.3) |
| cooperative matrix | 生の fragment 型を見せず、shared tile に対する collective(`group.matmul_acc` 相当)として出す。G6 | 速度の天井を決めるが Apple に届かない。CPU では普通の 3 重 loop で同じ意味になる(D15) |
| host API | `std::gpu` を Kizu source で書き、`extern "c" fn` で Vulkan を呼ぶ。複数 dispatch を 1 回の submit に束ねる `gpu::Batch` と、buffer から借りる mapped view を早い段で入れる | 推論 1 token で数百の dispatch が走り、dispatch ごとの submit+wait は遅すぎる。重みは GB 単位で、UMA では staging を省ける(§5.4) |
| 第一歩 | `out[i] = a[i] + b[i]`(f32 / u32)を lavapipe と実 GPU で走らせ、CPU で同じ関数を呼んだ結果と一致 | §7 |
| ML の最初の到達点 | G3: 素朴な kernel だけで llama 系 model の 1 token の forward が CPU 参照と一致する | 遅くてよいから全演算が書けることを先に示し、速さは G4〜G6 で積む(§7) |

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
  説明できる(§4)。`each_lane` は飛び飛びの要素の列(lane)を窓にし、
  `each_lane_with` は thread ごとの state を渡す。この 2 つが GPU の coalesced な書き込みと
  invocation ごとの register 状態にそのまま対応する(§4.2、§3.5)。
- `std::math` の `f32` は f64 で計算して 1 回丸める(`docs/std/math.md`)。f64 を持たない
  GPU ではこのままでは `exp` も書けない。同じ文書が「単精度専用の速い算法は要る利用者が
  現れてから差し替え、API は変えない」と書いており、GPU がその利用者になる(§3.3)。
- vector 型は 128-bit 固定名の 8 種で、f16 の lane を持たない。任意 lane 数の
  `Vector<N, T>` は持たない(SPEC §7.3、ADR-0149)。shape を型に書く案はこれに当たる(D15)。

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
- **ML の実績がある。** ggml-vulkan は 1 本の SPIR-V 群で NVIDIA / AMD / Intel /
  Qualcomm / Apple(MoltenVK)の LLM 推論を動かしており、要求する device 機能も
  Vulkan 1.2 + 16bit storage が下限で、残りは capability で分岐している(§9)。CUDA の
  速度に届かない部分は主に cooperative matrix(tensor core)の有無で、Vulkan でも
  `VK_KHR_cooperative_matrix` で取れる(D15)。
- macOS は MoltenVK の SPIR-V→MSL 変換を挟むので、第一級ではなく「届く」扱いになる。
  これは source に見せる制約として `docs/std/gpu.md` に書く。ML では差が具体的で、
  MoltenVK は cooperative matrix・bf16・f64 を出さない(§9.3)。Apple GPU の行列演算
  unit(Metal の simdgroup matrix)を使うには Metal backend が要り、それは D1 で
  後から足す別 target になる。
- OpenCL 系の SPIR-V(Kernel capability)は Vulkan が読めないので選ばない。
- PTX は ML の速度の天井が最も高いが NVIDIA だけで、Apple・AMD・Intel の利用者を
  切る。最初の 1 本にはしない。

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
- workgroup 単位の kernel(§3.5)は第 1 引数を `group: gpu::Group` にする。workgroup
  番号はその field で、workgroup 内の lane 番号は collective に渡す worker の引数にだけ
  現れる。lane 番号を kernel 本体の変数として見せない(§3.5 で理由を書く)。G4 で入れる。

### 3.3 kernel 内で使える言語機能

制限は新しい構文で言わず、wasm target と同じく「`export "gpu"` から到達する関数に
target が扱えない操作があれば build 時に拒否する」形にする(`spirv error:` で止まり、
他の backend へは切り替えない)。拒否の多くは型が先に閉じる。

ML を物差しにすると、旧版で「第一歩では拒否」にしていた小さい整数の storage と
f32 は最初の数段で要る(量子化重みは `u8` の列、活性化は f32)。逆に atomic は推論の
中心経路に要らない(§9.2)。表の「段」は §7 の段階。

| 機能 | 扱い | 段 | 理由 |
| --- | --- | --- | --- |
| 整数 `i32` / `u32` / `bool` | 使える | G1 | Vulkan core |
| `f32` | 使える。精度は D7 | G1 | ML の活性化・累積は f32 が基本。旧版の G3 から前倒し |
| `i64` / `u64` / `usize` | 使える(`shaderInt64` を必須) | G1 | slice の長さと添字が 64 bit のため。buffer は `maxStorageBufferRange`(多くの driver で 4 GiB 未満)に収まるので、範囲検査の後の address 計算は backend が 32 bit に落とせる。source の型は変えない |
| `i8` / `u8` / `i16` / `u16`(storage と演算) | **使える**(8 / 16bit storage と `shaderInt8` / `shaderInt16` を必須、D11) | G3 | 量子化 block は `u8` の列と `f16` の scale。ggml-vulkan も 8bit の storage access を前提にしている(§9.1)。必須にしても MoltenVK・lavapipe・desktop 3 社は落ちない |
| sub-byte(4bit / 2bit の量子化値) | 型を足さない。`u8` / `u32` に pack し、shift と mask で読む | G3 | ggml の Q4_0 / Q4_K も `uint8_t qs[]` に 2 値ずつ詰め、読む側で `& 0xF` / `>> 4` する(§9.2)。`i4` 型は Kizu の他 target に表現が無く、原理 6 に当たる |
| `f16` | 当面は型を足さない。storage は `u16`、f32 との変換は `std::float` の Kizu 関数(整数の bit 操作 + f32 の `from_bits`)。演算型としての `f16` は cooperative matrix と一緒に再判断 | G3 | ggml も fp16 演算は任意で、無ければ f32 で計算する(§9.1)。変換が整数演算なら `shaderFloat16` は要らない。変換は Kizu で書けるので primitive にしない(`docs/stdlib.md` の境界規則)。1 命令の変換より数命令重いが、律速は帯域なので後段まで待てる(D13) |
| `bf16` | `u16` に置き、f32 bit 列への shift で読む | G3 | 変換が bit 操作だけで書ける。`std::float::bits` / `from_bits` の f32 版が要る(D13) |
| 量子化 block の struct(`{d: u16, qs: [16]u8}` 等)を buffer の要素にする | 使える。layout は C layout(SPEC §12.2)をそのまま `Offset` / `ArrayStride` にし、std430 に収まらない layout は build 時に拒否 | G3 | 18 byte の Q4_0 block も std430 で表せる(最大 alignment 2、`u8` 配列の stride 1)。CPU 側と同じ byte 列を upload するだけで読める |
| 超越関数(`exp` / `tanh` / `erf` 等) | `std::math` の f32 版を f32 だけで計算する算法に差し替える。GPU 専用の名前分岐はしない | G3 | 今の f32 版は f64 経由(§1.5)。softmax / SiLU / GELU に要る。GLSL.std.450 の `Exp` に写す案は関数名で backend を分岐させる形になり原理 11 に当たる |
| vector 型 `f32x4` 等 | 使える | G3 | `OpTypeVector` にそのまま写る。16 byte 単位の読み込み(ggml の `vec4` / `LOAD_VEC_A`)がこれで書ける |
| struct / 固定長配列 / local `var` / `if` / `while` / `for` / `match` | 使える | G1〜G3 | Function storage の値 |
| 関数呼び出し | 使える。view を渡す呼び出しは backend が inline する | G3 | Logical addressing は StorageBuffer の pointer を関数引数にできない(VariablePointers 無しの場合)。fused kernel(RMSNorm+mul、§9.2)を部品の関数呼び出しで書けることが ML では重要 |
| workgroup の協調(shared memory、barrier、reduction) | 生では見せない。構造化 collective で出す(§3.5) | G4 / G5 | RMSNorm・softmax・行列ベクトル積の reduction と、tiled 行列積に必須(ggml の `.comp` 157 本のうち 58 本が直接 `shared` と `barrier()` を使い、行列ベクトル積などは include する共通部で使う) |
| subgroup 演算 | 直接は見せない。collective の lowering が使う | G4 以降 | 速さのための実装手段で、意味は collective が持つ(D14) |
| cooperative matrix | collective の 1 つとして出す。device 任意 | G6 | D15 |
| 整数 dot product(4×i8 → i32) | 普通の式(4 回の積和)で書ける。GPU の 1 命令(`OpSDotKHR`)に落とす方法は G6 で決める | G6 | ggml の q8_1 活性化 × 量子化重みの経路(`dotPacked4x8EXT`)。Vulkan 1.3 core の機能なので device 要件は増えない。Kizu で書けるものを速さのために primitive にするのは境界規則の例外になり、backend が式の形を認識するのは原理 11 に当たる。どちらも採らずに済む形があるかが論点 |
| atomic | 利用者には見せない(panic の status buffer は backend 内部で使う) | G7 | ggml でも atomic を使うのは 4 shader(MoE の expert 数え上げ、top-k、count_equal)だけで、dense model の推論経路には無い |
| `f64` | 拒否 | — | `shaderFloat64` は任意で、MoltenVK は出さない(§9.3) |
| 再帰 | 拒否 | — | SPIR-V が禁止。呼び出し graph の閉路で検出 |
| 関数 pointer(`fn(...)` 値) | 拒否。collective に渡す worker は関数名で直接書く(呼び先が静的に決まるので inline できる) | — | SPIR-V に間接呼び出しが無い |
| `Allocator` / `Io` / owner(`String` `Array` `Map` `Box` `Arena`) | 拒否(引数に取れない) | — | 確保は明示 allocator なので、kernel が allocator を受けられなければ確保は起きない |
| 文字列 literal / `print` | 拒否 | — | 出力先が無い |
| `extern "c"` / `std::thread` / `std::coro` | 拒否 | — | host の capability |
| `defer` / `errdefer` | 使える | G1 | 呼び出しが source にあり、制御は構造化される |
| error union を返す | kernel 自体は `void` のみ。内部関数の `!T` は後段で検討 | — | 失敗を host へ運ぶ ABI が要るため、まずは panic 経路だけ(§3.4) |
| raw pointer / `unsafe` | 拒否 | — | Logical addressing に任意 address が無い |

kernel の variant(量子化形式ごと、tile size ごと)は、generic な内部関数を書き、
`export "gpu" fn matvec_q4_0(...)` のような 1 行の薄い export で実体化する。ggml は
同じことを GLSL の preprocessor macro(`DATA_A_Q4_0` 等)と build 時の shader 生成で
している。Kizu では宣言側 1 行(原理 10)で、export を generic にする規則は足さない。

### 3.4 panic(範囲検査)の出口

GPU には trap も process 停止も無い(`OpKill` は fragment 専用)。案:

| 案 | 評価 |
| --- | --- |
| **a. status buffer に記録して invocation を return(推奨)** | target ABI が 1 つ storage buffer を予約し、panic した invocation が atomic に印を付けて抜ける。host の `run` は `gpu::Error::KernelPanicked` を返す。wasm の `kizu_start` が status を返すのと同じく、target ABI として文書化する(隠さない) |
| b. panic 経路のある kernel を拒否 | 範囲検査はほぼ全ての添字に入るので書けるものが無くなる |
| c. 範囲検査を GPU では外す | 原理 1 に反する |

他の invocation は走り続け、出力は部分的に書かれている。`run` が error を返したら
出力 buffer の中身を信じない、が契約になる。

### 3.5 workgroup の協調(reduction と tile)

invocation ごとの窓(§4.2)だけでは、1 行を足し合わせる RMSNorm・softmax・行列ベクトル積
の内積と、tile を shared memory に置く行列積が速く書けない。1 invocation が 1 行を逐次に
足す形なら窓の規則のまま書けるが(G3)、GPU の並列度を捨てる。

ここで欲しいのは 3 つで、生の shared memory と barrier ではどれも source の規則で
閉じられない:

1. shared memory への書き込みが lane 間で重ならない。
2. barrier が全 lane で同じ回数だけ実行される(発散した制御フローの中の barrier は
   SPIR-V で未定義動作)。
3. barrier の前に書いたものを後で読む、という順序が source に見える。

**推奨: `std::thread::each_lane_with` と同じ形の構造化 collective。** workgroup を pool、
lane を worker と読み替える。

- kernel 本体は workgroup に 1 つの**一様な**手続きとして書く。lane 番号は本体の変数に
  ならない。本体で読む値は全 lane で同じなので、本体の制御フローは構成上一様になり、
  規則 2 が型の段階で成り立つ。
- lane ごとの仕事は collective call(`group.each_lane(...)`、`group.sum(...)` 等)に
  関数名で渡す worker が持つ。worker は `each_lane` と同じく、lane に割り当てた飛び飛びの
  要素の列を `&var []T` で受ける。書き込みは自分の lane の窓だけなので規則 1 が成り立つ。
- lane ごとに持ち越す状態(行列積の累積 register)は `each_lane_with` の state と同じ
  形で、lane と同数の state を collective に渡す。
- **barrier は collective call の終わりにある。** `std::thread::each` が全部の塊が
  終わってから返るのと同じ意味で、call が source に見えるので原理 2 の内側にある
  (規則 3)。
- shared memory は kernel が local に持つ固定長の値(`gpu::Shared<[N]T>` 相当)で、
  collective の窓としてだけ書ける。読むのは collective の後で、そのときは普通の `&` で
  全 lane が読める。

```kizu
// 綴りは D14 で決める。形を示すための sketch。
/// 1 workgroup が x の 1 行を正規化して out の 1 行に書く。
export "gpu" fn rms_norm(group: gpu::Group, out: &var []f32, x: &[]f32, n: i64, eps: f32) -> void {
    let row = x[group.index * n..(group.index + 1) * n];
    let total = group.sum<f32>(row, square);           // lane に分配した和。終わりに barrier
    let scale = 1.0 / math::sqrt<f32>(total / cast<f32>(n) + eps);
    group.each_lane<f32>(out, row, scale, scale_into); // out の自分の lane だけを書く
    return;
}
```

CPU で呼ぶときは `gpu::Group` の CPU 版が lane を逐次に回す。`each_lane` を 1 thread で
回すのと同じで、意味は変わらない(和の順序は D7)。

backend の lowering は shared memory の tree reduction から始め、subgroup 演算は
後で lowering を置き換える(両方を device で切り替える 2 経路にはしない、D11、D14)。
協調の単位を workgroup にするので、subgroup の大きさ(8〜128、device ごとに違う)は
source に現れない。

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

連続の窓は `width > 1` のとき隣の invocation と離れた address を書くので、GPU では
coalesced にならず帯域を捨てる。ML の kernel は帯域で律速されるので、**飛び飛びの窓
(lane 窓)を連続の窓と並べて出す。** invocation `index` が要素
`index, index + N, index + 2N, …`(N は invocation 数)を `&var []T` として受ける。
`std::thread::each_lane` と同じ窓で、CPU では std が lane を集めて連続の slice にし、
書き戻す。GPU の backend は集めずに、添字 `k` を `index + k * N` に写すだけでよい。
kernel から見た型は `&var []T` のままで、どちらの窓かは host 側の `args` が決める。

窓で書けないもののうち、reduction と tile は §3.5 の collective で出す。scatter と
histogram は atomic が要り、推論の中心経路には無いので後段に置く(G7、§8 D5)。

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
- 複数 dispatch を束ねる `gpu::Batch`(§5.4)も、submit して完了を待つ call
  (`batch.run(io)`)までの間 buffer を借りる値で、借用は同じ規則で閉じる。
- 非同期 dispatch(返り値を後で待つ)は `std::io::Future` と同じく、貸した buffer に
  tie した値として後段で設計する(§7 G7)。in-flight の buffer に host が触れないことを
  borrow で閉じる。
- mapped view(§5.4)は `gpu::Buffer<T>` から借りた `&var []T` / `&[]T` で、生きている
  間はその buffer を `run` / `Batch` に貸せない。host と device が同時に同じ memory を
  触ることを、新しい規則でなく普通の `&var` と `&` の重なりの拒否で閉じる。

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
`BufferTooLarge`(1 つの buffer が `maxStorageBufferRange` を超える。memory 不足とは
回復が違う)、`OutOfMemory`(host)、`DeviceLost`、`KernelPanicked`、`LengthMismatch`、
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

### 5.4 推論向けの host 側論点

| 論点 | 範囲 | 段 | 理由 |
| --- | --- | --- | --- |
| dispatch の束ね(`gpu::Batch`) | **入れる** | G3 | 推論 1 token は層ごとに数十の演算が並び、dispatch ごとに submit して待つと latency が支配する。ggml は 100 node ごとに submit し、途中で待たない(§9.4)。`Batch` は dispatch を記録して 1 回で submit・完了待ちする。dispatch 間には compute→compute の全体 barrier を毎回置き、省く最適化は後段。barrier の省略を推論しないので、記録した順序がそのまま実行順になる(D16) |
| 大容量の重み buffer | **入れる** | G3 | model の重みは GB 単位。1 buffer は `maxStorageBufferRange`(MoltenVK は 4 GiB − 1 に切り詰める、§9.3)と `maxMemoryAllocationSize` を超えられないので、tensor ごとに buffer を作る形を基本にし、超えたら `BufferTooLarge`。ggml は 1 GiB 単位で sub-allocate している(§9.4)。sub-allocation の器(`gpu::Heap`)は確保回数の上限(`maxMemoryAllocationCount`)に当たってから足す |
| mapped memory | **入れる** | G3 | UMA(Apple、内蔵 GPU)では device-local かつ host-visible な memory があり、重みを file から直接そこへ読める。discrete GPU でも staging を自前で持たずに済む。`buffer.map()` が buffer から借りた view を返し、借用の重なりで同時 access を閉じる(§4.4、D6)。memory 種別は buffer 作成時の引数で明示する |
| pinned host memory | 入れない(mapped の host-visible buffer で代える) | — | Vulkan の pinned は host-visible な device memory を map したものと同じで、別の概念を足す理由がない。既存の host slice を import する `VK_EXT_external_memory_host` は ggml でも MoltenVK で無効化されている(§9.4) |
| 転送と dispatch の重ね合わせ | 後段 | G7 | 推論中の転送は token と logits だけで小さい。重みは起動時に 1 回。非同期 dispatch と同時に入れ、状態機械は Quint で見る(§6.4) |
| device の選択 | **入れる**(列挙と番号での open) | G3 | `gpu::devices(io, allocator)` が名前・種別(discrete / integrated)・memory 量・任意機能の有無を返し、`gpu::open_index` で選ぶ。ggml も `GGML_VK_VISIBLE_DEVICES` で選ばせる。既定は「最初の discrete、無ければ最初」で、選んだ規則を `docs/std/gpu.md` に書く |
| 複数 GPU への分割 | 範囲外 | — | 層を device に分ける・tensor を分ける判断は推論 engine 側の設計で、言語と std は「複数の `Device` を同時に開ける」ことだけ保証すればよい。device 間の直接転送(peer copy)は Vulkan の標準に無い |

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
- collective の和(§3.5)は lane 数と subgroup の大きさで足す順序が変わり、CPU の逐次和と
  ULP 単位では合わない。比較は和の誤差上界(要素数 × ε × Σ|x| の程度)で行う(D7)。
- ML の end-to-end は、llama 系の小さい model で 1 token の logits を CPU 参照と比べ、
  argmax(greedy の次 token)の一致と logits の相対誤差で判定する。量子化重みの
  dequantize は整数演算と f16→f32 変換だけなので完全一致を要求できる。

### 6.3 ベンダー・driver 差

- `gpu::open` が device の version と必要 feature(D11 の必須集合)を問い、
  欠けていれば `MissingFeature` で止める。足りない機能を emulation しない。
- 任意機能(cooperative matrix 等)は `device.has(gpu::Feature::...)` で host が問い、
  どの kernel を走らせるかを source で選ぶ。任意機能を使う kernel を持たない device で
  `run` すると `MissingFeature`。backend や `run` が黙って別 kernel に替えることはしない。
- CI は Linux の lavapipe(Mesa の CPU 実装 Vulkan)で回す。GitHub の runner に GPU は
  無い。必須にしない別 job にする(direct の差分 job と同じ扱い)。lavapipe は 8 / 16bit
  storage、subgroup size control、`VK_KHR_cooperative_matrix` まで実装しているので
  (§9.3)、cooperative matrix の kernel も GPU 無しで正しさを検査できる。
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

ML-first に組み替えた。旧版との違いは 3 つ: f32 を G1 に、小さい整数の storage・
dispatch の束ね・mapped memory を G3 に前倒しし、workgroup の協調を G5 から G4 / G5 に
分けて先に置いた。atomic と非同期は推論の中心経路に要らないので最後(G7)に下げた。

| 段 | 内容 | 書けるようになる llama.cpp の処理 | 完了の判定 | 規模 |
| --- | --- | --- | --- | --- |
| G0 | `spirv-tools` と lavapipe を devShell に入れ、手書きの最小 SPIR-V を host 側の小さい Kizu program(`extern "c"` で Vulkan)から走らせる | — | lavapipe と手元の GPU で結果が読める | 小(数百行、std に入れない検証用) |
| **G1(第一歩)** | `export "gpu"`、`--target gpu`(名前は D1)、`spirv` backend の最小形(`u32` / `i64` / `bool` / `f32`、`if` / `while` / `for`、`&[]T` / `&var []T` 引数、push constant の scalar 引数、範囲検査 → status buffer)、`std::gpu` の open / buffer / upload / download / run | 要素ごとの `add` / `mul` / `scale` | `examples/gpu/add.kizu` が `out[i] = a[i] + b[i]`(f32 と u32)を GPU で計算し、同じ関数を CPU で呼んだ結果と一致し、`spirv-val` を通る | 中(backend 3〜5k 行、std::gpu 1.5〜3k 行 —— Vulkan の初期化が大半) |
| G2 | 検証の常設: `just gpu-smoke`、`difftest-gpu`、lavapipe の CI job | — | 全 gpu example が smoke と差分を通る | 小 |
| **G3(ML の最初の到達点)** | 8 / 16bit の整数、struct の buffer 要素、vector 型、関数呼び出しの inline、`match`、lane 窓、f32 の `bits` / `from_bits` と f16 / bf16 の変換関数、f32 だけで計算する `std::math`、2 次元 index、`gpu::Batch`、mapped memory、device の列挙 | 全演算の素朴版: 埋め込みの行引き(`get_rows`、Q8_0 / Q4_0 の dequantize 込み)、1 invocation = 1 出力要素の行列ベクトル積・行列積(f32 / f16 / Q4_0 / Q8_0)、1 invocation = 1 行の RMSNorm・softmax、RoPE、SiLU / GELU、要素ごとの演算、1 head を逐次に回す attention | llama 系の小さい model の 1 token の forward を GPU で走らせ、logits が CPU で同じ kernel を呼んだ結果と §6.2 の基準で一致する。速さは問わない | 大(backend 3〜5k 行、std 2〜3k 行。model を読む推論 program 自体は example 側) |
| G4 | workgroup kernel(`gpu::Group`)と collective の reduction(`sum` / `max`)と lane 分配(§3.5)。lowering は shared memory の tree | 行列ベクトル積の内積を workgroup で分担(token 生成の律速部分)、RMSNorm・softmax を 1 workgroup = 1 行で。RMSNorm+mul のような fused kernel | 同じ model で token 生成の速さを測り、G3 からの改善を PR に書く | 中〜大 |
| G5 | `gpu::Shared` と lane ごとの state(`each_lane_with` 相当)、tile を跨ぐ一様 loop | tiled 行列積(prompt 処理)、flash attention 型の fused attention | prompt 処理の速さを測る。tiled 行列積が素朴版と一致する | 大 |
| G6 | cooperative matrix の collective(D15)、`f16` 演算型の再判断(D13)、subgroup 演算への lowering の置き換え(D11)、整数 dot product | ggml の `mul_mm` coopmat 経路・`mul_mat_vecq` 相当 | NVIDIA / AMD RDNA3 / Intel の実機で coopmat 版が非 coopmat 版と一致し、速さを PR に書く。lavapipe でも一致 | 大 |
| G7 | 非同期 dispatch と転送の重ね合わせ(Quint 模型)、atomic、compile 時の署名照合(D4 の再判断)、kernel 内の `!T` | MoE の expert 振り分け(`count_experts`、`topk_moe`)、argmax / top-k の GPU 化 | 各項目が別の設計判断 | 大 |

G1 の時点で SPEC に足すのは §12 の GPU ABI 節(型の表、窓の規則、status buffer)と
§13.3 の target 述語 1 つ(`std::target::is_gpu()`)。`docs/std/gpu.md` を新設する。

## 8. 決めること

| # | 論点 | 選択肢 | 推奨 | 採用時の影響(範囲 / コスト感) |
| --- | --- | --- | --- | --- |
| **D1** | target 形式と backend の置き場所 | (a) SPIR-V を新 backend で直接書く (b) LLVM の `spirv-unknown-vulkan1.3-compute` に乗る (c) PTX (d) Metal | **(a)** | (a) `compiler/src/internal/spirv/` 新設、wasm の構造解析を共有。外部 toolchain 無し。(b) LLVM backend に GPU 用の別 lowering と HLSL 系 resource intrinsic が要り、clang の wrapper も外す。runtime 前提を外す量は (a) と変わらず、動いている API に依存する |
| **D2** | 入口の綴り | (a) `export "gpu"` (b) `export "spirv"` (c) `export "vulkan"` (d) 属性 `@compute` | **(a)** | type_collection の ABI 名を 1 つ足すだけ。(b)(c) は将来 Metal を足すとき source を書き換えさせる |
| **D3** | invocation 番号 | (a) kernel の第 1 引数 (b) `@global_id()` builtin (c) compiler 定義の std 関数 | **(a)** | 構文変更なし、kernel を CPU で呼べる。(b) は parser・型検査・IR・全 backend に「kernel 文脈」の規則が要り、`@embed_file`(load 時定数)とは別物 |
| **D4** | host build と kernel の結び方 | (a) 別 build:`kizu build --target gpu` で `.spv` を作り host が `@embed_file` (b) 1 回の native build が `export "gpu"` を spirv backend にも通し、`std::gpu` が compiler 定義の form で module と署名を引く | **G1 は (a)、G7 で (b) を再判断** | (a) 言語変更なし。ただし build script を持たない(SPEC §2)ので 2 コマンドになり、`.spv` が古くなりうる。署名照合は実行時。(b) 署名を compile 時に照合できる。1 build に 2 backend が走る初めての形で、cache key と到達可能性が target 2 つ分になる。native build で `export "gpu"` を拒否せず普通の関数として扱う規則も要る |
| **D5** | 書き込みの安全規則 | (a) 窓(重ならない `&var []T`)だけ。連続窓と lane 窓の 2 種、workgroup 内は §3.5 の collective。scatter は atomic と一緒に後で (b) 最初から raw pointer + `unsafe` を許す (c) 書き込みを全部 `unsafe` | **(a)**(旧版から lane 窓と collective を足した) | data race を型で閉じる(SPEC §15 の方針を GPU でも守る)。lane 窓は `each_lane` と同じ意味なので SPEC の新規則は窓の写し方だけ。ML の推論経路で窓に収まらないのは MoE の振り分けと top-k くらいで、G7 まで待てる |
| **D6** | host と device のメモリ | (a) 別型 `gpu::Buffer<T>` と明示転送だけ (b) unified / mapped memory を `[]T` に見せる (c) (a) に加え、`buffer.map()` が buffer から借りた view を返す | **(c)**(旧版の (a) から変更) | 転送は source に見える(原理 2)まま、UMA で重みを staging 無しに読める。同時 access は借用の重なりで拒否され、新しい規則は要らない。(b) は転送と同期が隠れる |
| **D7** | GPU 上の浮動小数の意味 | (a) SPEC に「gpu target の `f32` は Vulkan の精度要件」と書いて緩める (b) CPU と同じ意味を守り、満たせない演算を拒否 (c) `+ - *` は CPU と一致させ(`NoContraction`)、`/` の誤差(Vulkan は 2.5 ULP)と denormal の flush と collective の和の順序だけを SPEC で緩める | **(c)**(旧版の (a) を絞った) | 超越関数を f32 の `+ - *` で書く限り CPU と bit 一致する範囲が広く残り、差分検査が厳しく保てる。和の順序は「collective の和の順序は未規定」と 1 行で書く。縮約を止める分 FMA の速さを失うので、`math::fma` を明示的に書く経路の扱いは別に要る(Vulkan の `Fma` は融合を保証しない)。(b) は割り算すら書けなくなる |
| **D8** | Vulkan の取り込み方 | (a) `@link_library("vulkan")` (b) `dlopen` + `vkGetInstanceProcAddr` (c) trusted primitive(runtime の C) | **(b)** | (b) Vulkan が無い machine でも起動し `NoDevice` を返せる。`dlopen` / `dlsym` の宣言と、関数 pointer 表の初期化コードが std に入る。(a) は起動失敗になる。(c) は `docs/stdlib.md` の「Kizu で書けるものは primitive にしない」に反する |
| D9 | 引数の渡し方 | (a) descriptor + Logical addressing (b) buffer device address + push constant | **(a)** | (a) driver 差が小さく `robustBufferAccess` が効く。backend は view を (binding, offset, len) で追い、view を渡す call を inline する。(b) backend は単純だが OOB が本物の破壊になる |
| D10 | panic の出口 | (a) status buffer に印を付けて return (b) panic 経路のある kernel を拒否 | **(a)** | target ABI に予約 binding が 1 つ増え、SPEC の GPU ABI 節に書く |
| **D11** | device 要件 | (a) Vulkan 1.3 + `shaderInt64`(旧版) (b) (a) + `storageBuffer8BitAccess` / `storageBuffer16BitAccess` + `shaderInt8` / `shaderInt16` を必須、cooperative matrix・`shaderFloat16`・bf16 は任意 (c) (b) + subgroup arithmetic も必須 (d) ggml と同じく Vulkan 1.2 + 16bit storage を下限に、残りを全部 capability で分岐 | **(b)**。subgroup arithmetic は G6 で lowering を置き換えるときに (c) へ上げるか再判断 | (b) で落ちるのは 8bit storage の無い古い mobile GPU(Mesa の turnip では Adreno a750 未満、§9.3)くらいで、MoltenVK・lavapipe・desktop 3 社は残る。backend は型を 1 対 1 で写せる。(d) は kernel ごとに機能の有無で分岐する経路を量産する(原理 9)。subgroup arithmetic は MoltenVK で GPU 次第(Metal の simd reduction がある GPU だけ)なので、必須化すると古い Mac が落ちる |
| D12 | workgroup size と tile size | (a) workgroup size は specialization constant にし host の `run` が選ぶ。tile size 等は comptime 定数で variant を実体化 (b) source に書く(属性) (c) compiler 固定 | **(a)** | source の構文が要らない。`LocalSizeId` が Vulkan 1.3 core なので D11 とつながる。ggml は tile size も specialization constant で device ごとに変えるが、Kizu では generic な内部関数と 1 行の export で variant を作る(§3.3)。device ごとの自動選択は host が明示的に書く |
| **D13** | f16 / bf16 の位置づけ | (a) 型を足さない。storage は `u16`。trusted primitive は `std::float::bits` / `from_bits` の f32 版だけ足し、f16 / bf16 ↔ f32 の変換はその上の Kizu 関数として `std::float` に置く (b) `f16` を言語の scalar 型として足す(全 backend、literal、cast、`std::math`) (c) GPU target だけの `f16` 型 | **G3 は (a)。cooperative matrix(D15)を入れる G6 で (b) を再判断** | (a) は f32 の bit 読み書きの primitive 2 つだけで、`std::float::bits` と同じ先例に乗る。ggml も fp16 演算は任意で、変換だけなら `shaderFloat16` は要らない。ただし `u16` が「f16 の bit 列」を意味するのは区別が型に無い状態で、cooperative matrix の入力や f16 累積を扱う段では原理 7 に当たる。(b) は native 3 backend と wasm に f16 を足す費用(arm64 は命令がある、amd64 と wasm は変換を自前で書く)。(c) は CPU で kernel を呼べなくなる |
| **D14** | workgroup の協調の安全モデル | (a) `each_lane_with` 型の構造化 collective(kernel 本体は一様、lane の仕事は関数名で渡す worker、barrier は call の終わり)(§3.5) (b) ISPC 型の uniform / varying を型に足す (c) 生の shared memory と barrier を `unsafe` で出す (d) reduction・行列積を std の固定演算(primitive)だけで出す | **(a)** | (a) data race と barrier の発散が書けない。CPU で同じ意味に走る。SPEC に要るのは `gpu::Group` の collective が barrier を含むことと、worker を関数名で渡す規則。(b) 全ての値に区別を課す(原理 6)。(c) 未定義動作が source に書けてしまい、review は `unsafe` を grep できても正しさを確かめられない。(d) RMSNorm+mul のような fused kernel を利用者が書けず、std が ggml の op 一覧になる |
| **D15** | cooperative matrix(tensor core 系)の載せ方 | (a) fragment を不透明な専用型で見せる(固定名の型と load / store / mul_add) (b) `gpu::Shared` の tile に対する collective `group.matmul_acc(state, a_tile, b_tile)` として出し、fragment は lane の state に隠す (c) device の fragment layout を per-lane の配列として見せる intrinsic (d) 持たない | **(b)。G6 で入れる。device 任意で、無い device では `MissingFeature`。非 coopmat 版の kernel は利用者が別に書き、host が `device.has` で選ぶ** | (b) 新しい値の型を言語に足さず、§3.5 の collective が 1 つ増えるだけ。CPU では tile の 3 重 loop として同じ意味に走る。tile の大きさは device の対応 shape(多くは 16×16×16)の倍数を要し、pipeline 作成時に照合する。(a) shape が device ごとに違い、固定名の型にすると `Vector<N, T>` を持たない方針(ADR-0149)と当たる。(c) 移植性が無い。backend が coopmat と scalar を device で黙って切り替える案は、精度(f16 入力・累積順)が変わる 2 経路になるので採らない(原理 9)。到達範囲: NVIDIA(Turing 以降)・AMD(RDNA3 以降)・Intel・lavapipe で使え、**MoltenVK には無い**(§9.3)。Apple GPU で行列演算 unit を使うには Metal backend が要る |
| **D16** | 複数 dispatch の束ね方 | (a) `gpu::Batch` に dispatch を記録して 1 回 submit・完了待ち。dispatch 間は毎回全体 barrier (b) (a) で barrier を buffer の読み書きから推論して省く (c) 最初から非同期 submit と Future | **(a)。G3** | 推論 1 token の数百 dispatch で submit+wait の往復を消せる。順序が記録順のまま見える。(b) は barrier の有無が source に見えない最適化で、測ってから入れる。(c) は in-flight 状態の借用と Quint 模型が要り、G7 に置く |
| **D17** | 大容量 buffer の扱い | (a) 1 buffer = 1 tensor を基本にし、`maxStorageBufferRange` を超えたら `BufferTooLarge` (b) 最初から sub-allocation の `gpu::Heap` (c) buffer device address(PhysicalStorageBuffer64)で 4 GiB 超を 1 本で扱う | **(a)。確保回数の上限に当たったら (b)** | (a) 型も規則も増えない。大きい model でも 1 tensor は数 GiB 未満に収まるのが普通。(c) は D9 の (b) と同じ理由(OOB が本物の破壊になる)で採らない |
| D18 | device の選択と複数 GPU | (a) 列挙と番号での open を std に入れ、分割は利用者の program が複数 `Device` を開いて行う (b) std が複数 GPU への分割まで持つ | **(a)** | (a) std の仕事は device を名指せることまで。分割の方針は推論 engine ごとに違う。device 間の直接転送は Vulkan 標準に無く、host 経由の転送が source に見える |

最重要は D1(backend の置き場所 —— 実装量の大半を決める)、**D14(workgroup の協調 ——
ML の速さの大半を決め、言語の規則として最も新しい)**、D3(番号を引数で受ける —— CPU
参照の成立)、D15(cooperative matrix —— 速度の天井と Apple への到達が引き換えになる)。

## 9. 参照ワークロード: ggml-vulkan の実態

llama.cpp の Vulkan backend を source で読んだ結果です(2026-11-14 時点の
`ggml-org/llama.cpp` master、commit `9183a552`)。行番号はその commit のもの。

### 9.1 device への要求

| 機能 | 扱い | 根拠 |
| --- | --- | --- |
| Vulkan 1.2 | **必須**。未満は初期化で error | `ggml-vulkan.cpp:5493` |
| `storageBuffer16BitAccess` | **必須**。無い device は `Unsupported device` | `ggml-vulkan.cpp:4925` |
| 8bit の storage access | feature の確認なしに前提。量子化 block の struct が `uint8_t` member を storage buffer に置く。共通 header は `GL_EXT_shader_explicit_arithmetic_types_int8/16/64` を `require` | `vulkan-shaders/types.glsl:4-8`、`:60-64`(`block_q4_0 { float16_t d; uint8_t qs[16]; }`) |
| `shaderFloat16` | 任意。16bit storage と f16 演算と `shaderFloat16` が揃えば fp16 で計算し、無ければ f32 で計算 | `ggml-vulkan.cpp:4771` |
| `shaderInt64` | 任意(問い合わせて記録) | `ggml-vulkan.cpp:4800` |
| subgroup の basic / arithmetic / shuffle / ballot / vote / clustered | すべて任意。kernel ごとに有無で shared memory 版と切り替える。MoltenVK + AMD GPU では arithmetic を無効化する回避策がある | `ggml-vulkan.cpp:4498-4523`、`:3458` |
| `VK_EXT_subgroup_size_control` | 任意。vendor ごとの tile 選択に使う | `ggml-vulkan.cpp:4316`、`:1892` |
| `VK_KHR_cooperative_matrix` / `VK_NV_cooperative_matrix2` | 任意。行列積と flash attention の別経路(`mul_mm_cm2.comp`、`flash_attn_cm1/cm2.comp`) | `ggml-vulkan.cpp:4321`、`:4914-4918` |
| `VK_KHR_shader_integer_dot_product` | 任意。q8_1 に量子化した活性化 × 量子化重みの内積(`dotPacked4x8EXT`) | `ggml-vulkan.cpp:4335`、`mul_mat_vecq.comp` |
| `VK_KHR_shader_bfloat16` | 任意 | `ggml-vulkan.cpp` の `bf16` 判定 |
| buffer device address | 任意。cooperative matrix 2 の経路と 64 bit 添字でだけ使う | `vulkan-shaders/mul_mm_cm2.comp:17` |

ggml の方針は「下限を低く、残りは全部 device ごとに分岐」で、そのために同じ演算の
shader variant を大量に持つ(約 190 本の shader source から build 時に生成)。Kizu は原理 9 から下限を
上げて分岐を減らす側を取る(D11)。

### 9.2 カーネルの種類と要る言語機能

| kernel | 代表 file | 要る機能 |
| --- | --- | --- |
| 量子化行列ベクトル積(token 生成の律速) | `mul_mat_vec_*.comp`、`mul_mat_vecq.comp` | `u8` / `u16` の storage、shift と mask による 4bit 値の取り出し、f16 scale の変換、workgroup 内の reduction(subgroup か shared memory) |
| 行列積(prompt 処理) | `mul_mm.comp`、`mul_mmq.comp`、`mul_mm_cm2.comp` | shared memory の tile、barrier、thread ごとの累積 register、16 byte の vector 読み込み、tile size の specialization constant、任意で cooperative matrix |
| 量子化の展開 | `dequant_q4_0.comp` ほか 26 本 | 量子化 block の struct 読み、bit 操作 |
| RMSNorm | `rms_norm.comp` | 1 行を workgroup で reduction。RMSNorm+mul(+add)の fused 版が別 pipeline(`ggml-vulkan.cpp:3466-3468`) |
| softmax | `soft_max.comp` | 行内の max と和の 2 回の reduction、`exp` |
| RoPE | `rope_norm.comp`、`rope_neox.comp` | 要素ごと。`sin` / `cos`、push constant の位置と周波数 |
| attention | `flash_attn.comp`、`flash_attn_cm1/cm2.comp` | shared memory、online softmax、任意で cooperative matrix |
| MoE の振り分け、top-k | `count_experts.comp`、`topk_*.comp` | atomic。atomic を使う shader はこの 4 本だけ |

`.comp` 157 本のうち 58 本が直接 `shared` を宣言する。`.comp` と共通部の `.glsl`
(計 187 本)では 52 本が `constant_id`(specialization constant)を、60 本が push constant
を使う。

### 9.3 到達範囲(MoltenVK と Mesa)

MoltenVK は source(`MVKDevice.mm`、2026-10 の master `5189f8de`)、Mesa は
`docs/features.txt` で確かめた。

| 機能 | MoltenVK | lavapipe | 備考 |
| --- | --- | --- | --- |
| 8 / 16bit storage、`shaderInt8` / `shaderInt16` | あり(`MVKDevice.mm:158`、`:178`、`:184`、`:2816`) | あり | Mesa turnip は 16bit が a650+、8bit が a750+ |
| `shaderFloat16` | あり(`:183`) | あり | |
| `shaderInt64` | あり(Apple3+ / Mac1、`:2842`、`:2864`) | あり | |
| `shaderFloat64` | **なし** | あり | Kizu は GPU で f64 を拒否する(§3.3) |
| subgroup arithmetic | GPU 次第(Metal の simd reduction があるときだけ、`:1503`)。clustered も同様 | あり | |
| subgroup size control | GPU 次第(`:236`) | あり | |
| integer dot product | あり(`:242`) | あり | Vulkan 1.3 core |
| `VK_KHR_cooperative_matrix` | **なし**(extension 一覧 `MVKExtensions.def` に無い) | あり | radv は gfx11(RDNA3)+、nvk は Turing+、anv あり |
| bf16(`VK_KHR_shader_bfloat16`) | **なし** | なし | anv gfx12.5+、radv gfx12+ |
| `maxStorageBufferRange` | 4 GiB − 1 に切り詰め(`:2959`) | — | 1 buffer の上限(D17) |
| `VK_EXT_external_memory_host` | extension はあるが ggml は MoltenVK で無効化(`ggml-vulkan.cpp:4216`) | — | §5.4 |

要点: D11 の (b) を必須にしても MoltenVK と lavapipe は残る。MoltenVK で失うのは
cooperative matrix・bf16・f64 で、Apple GPU の LLM 推論は「coopmat 無しの kernel で
動く」扱いになる。lavapipe は cooperative matrix まで持つので、CI で全経路の正しさを
GPU 無しに見られる。

### 9.4 ML 推論で一般に効く要件(ggml 固有でないもの)

- **帯域**: token 生成は重みを 1 回読むだけの行列ベクトル積が支配し、memory 帯域で
  律速する。coalesced な読み書き(lane 窓、§4.2)と、量子化で読む byte を減らすこと
  (`u8` storage と sub-byte の pack)が計算機能より先に効く。
- **dispatch の往復**: 演算数が多く 1 つ 1 つは小さいので、submit と完了待ちの往復を
  束ねる(ggml は既定で 100 node ごとに submit、`ggml-vulkan.cpp:4525`)。
- **fused kernel の書きやすさ**: RMSNorm+mul、rope の前段の融合など、memory への
  往復を減らす融合が速さに直結する。部品を関数として書き、kernel が呼んで inline される
  こと(§3.3)と、collective の中から部品を呼べること(§3.5)で書ける。
- **非同期転送**: 推論中の転送は小さく、重みの転送は起動時 1 回。重ね合わせの優先度は
  低い(§5.4)。
- **大容量の確保**: ggml は 1 GiB 単位で sub-allocate し(`ggml-vulkan.cpp:4478`)、
  UMA では host-visible な device memory を直接使う(`ggml-vulkan-buffers.cpp:226`)。

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
- ggml-vulkan(§9.1、§9.2 の行番号はこの commit):
  https://github.com/ggml-org/llama.cpp/tree/9183a552e5fdcbdb5243f187f3a63204eca7dba9/ggml/src/ggml-vulkan
- MoltenVK の device feature(§9.3):
  https://github.com/KhronosGroup/MoltenVK/blob/5189f8de86ec365860f63bf1da736815cfaea5cd/MoltenVK/MoltenVK/GPUObjects/MVKDevice.mm
- MoltenVK の extension 一覧:
  https://github.com/KhronosGroup/MoltenVK/blob/5189f8de86ec365860f63bf1da736815cfaea5cd/MoltenVK/MoltenVK/Layers/MVKExtensions.def
- Mesa の driver ごとの Vulkan 実装状況(lavapipe = `lvp`):
  https://gitlab.freedesktop.org/mesa/mesa/-/blob/main/docs/features.txt
