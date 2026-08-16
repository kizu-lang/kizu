# ADR-0092: Allocator は opaque capability のまま実体化する

Status: 提案(ADR-0091 の決定を反映して改稿予定)

Issue: #549

## 背景

`Allocator` は SPEC 上「visible opaque capability type」(SPEC.md:1816)だが、
runtime の実体は NULL token である(`internal/native/build.go:730` の
`mem_page_allocator` は `return NULL`)。Array / Arena / String は渡された
allocator を無視して直接 malloc / realloc を呼ぶ。つまり「明示 allocator」は
今のところ型検査上の契約だけで、runtime には存在しない。

判断の土台にした 3 つの事実:

1. **contract / dyn は `&dyn` の borrow dispatch 限定で、borrow field は禁止。**
   container は deinit まで allocator を保持する必要があるため、user 実装
   allocator への参照を struct に持てない。Zig 式 vtable は「user state への
   追跡されない raw pointer を container が保持する」形であり、Kizu の
   安全公理と構造的に衝突する。
2. **leak は compile error ではない**(ADR-0090 事実 2)。deinit は source 上の
   明示契約であり、checker は leak を追わない。leak 検出の受け皿は runtime
   側にしか置けない。
3. **double deinit は checker が静的に防いでいる。** deinit は owned local
   receiver 限定で、moved 追跡が再使用を拒否する。runtime での double-free
   検出は不要である。

## 決定

### 1. user-defined allocator は当面入れない(opaque 維持)

`Allocator` は user が実装できる contract にせず、std factory だけが作れる
opaque capability のまま保つ。理由は可逆性の非対称にある。

- 閉 → 開は additive(contract 化して開けば既存コードは全部通る)
- 開 → 閉は breaking(container が user state を参照する形が既成事実になり、
  owned dyn / field borrow の将来設計を先取りで縛る)

「自作 allocator でなければ満たせない実需」(特殊メモリ、workload 特化戦略)は
現ユーザー(selfhost と examples)に存在しない。pool 系は `std::arena` が、
leak 検出は決定 3 が、heap のない環境は fixed_buffer(延期、決定 4)と
freestanding backend(scope 外、決定 5)がそれぞれ受ける。

### 2. Allocator を実体のある handle にする

NULL token をやめ、kind と state を持つ runtime handle にする。
Array / String / Map / Box / Arena のすべての割り当て・解放は、構築時に
渡された handle 経由で行う。`page_allocator()` の観測可能な挙動は変えない。

これで「明示 allocator」が仕様上の擬制でなくなり、allocator の種類を増やす
観測点ができる。allocation ごとに kind 分岐 1 つが入るが、page allocator が
既定経路である限り分岐予測で消える。

### 3. `testing_allocator()` を追加する

```text
std::mem::testing_allocator() -> Allocator
```

呼び出しごとに独立した追跡 state を作り、outstanding allocation を数える。
`kizu test` は test block 終了時に、その test 内で作られた testing allocator に
未解放 allocation が残っていれば test を失敗として報告する。

終了時の自動検査は hidden control flow ではない: opt-in は
`testing_allocator()` の呼び出しで source 上に明示されており、報告は test
runner の仕事である(`testing::expect` の失敗報告と同じ層)。検出は leak のみ
とする(double-free は事実 3 のとおり静的に防がれている)。

### 4. `fixed_buffer_allocator(buf)` は延期する

copy 型の `Allocator` が user の buffer を borrow する形になり、borrow field
禁止と衝突する。mutable slice / buffer provenance の仕様(SPEC が既に別途
延期中)が決まってから設計する。

### 5. freestanding backend 差し替えは #549 の scope 外

kernel / `--libc off` 環境で必要なのは per-container の差し替えではなく、
`page_allocator()` の実体を build 単位で供給すること(Rust の
`#[global_allocator]` 相当)である。これは borrow field 問題を持たず、
freestanding build(SPEC §17)の設計に属する別機構として扱う。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `contract Allocator`(Zig 式全面開放) | borrow field 禁止と衝突。owned dyn 未実装。user state の寿命を誰も追跡せず dangling allocator を許すことになる |
| NULL token のまま testing_allocator だけ足す | allocation が handle を経由しないため観測点がなく、実装できない |
| 自動検査をやめ明示 query のみにする | query を忘れた test が黙って pass し、leak 検出という目的を損なう |
| hidden default / global allocator | 明示 allocator の公理(SPEC.md:1828)と矛盾 |

## 影響

- `internal/native/build.go`: allocator handle 型の導入、
  `kizu_array_*` / `kizu_arena_*` / String / Map / Box の割り当て経路を
  handle 経由に統一
- `internal/stdprim`: `mem_testing_allocator` の追加
- `lib/kizu/std/src/mem.kizu`: `testing_allocator()` の追加
- SPEC.md: `#549 で別途仕様化します` の行(SPEC.md:1832)を本決定へ置き換え
- 正例は `tests/behavior/`(leak なし)、負例は leak を仕込んだ test の失敗を
  conformance で確認

## 再評価条件

- owned dyn または field borrow 追跡が入った時、決定 1(contract 化)を再検討
- mutable slice / buffer provenance の仕様化後、決定 4(fixed_buffer)を設計
- freestanding build の設計時に、決定 5 の backend 供給機構を仕様化
