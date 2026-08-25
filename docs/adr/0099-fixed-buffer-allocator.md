# ADR-0099: fixed_buffer allocator と allocator tie の構造的導出

Status: 採用

Issue: なし(#538 起点の tied 値探索の帰結。ADR-0092 決定 2・3 の履行)

## 背景

ADR-0092 は fixed_buffer を「mutable slice / buffer provenance の仕様化待ち」
として延期し(決定 3)、handle 実体化を「第二の allocator 種を入れる変更が
同時に行う」とした(決定 2)。ADR-0096(mutable slice)、ADR-0097(stack
buffer)、ADR-0098(構造的 tie 導出)で前提がすべて揃った。

延期の核心だった「copy 型の `Allocator` が user の buffer を borrow する形は
borrow field 禁止と衝突する」は、Allocator を user struct でなく **view と
同類の tied 値**として扱うことで解消する。`[]u8` が pointer を内包しながら
checker の tie 追跡で安全なのと同じ構図である。

## 決定

### 1. `std::mem::fixed_buffer(bytes: &var []u8) -> Allocator`

stack buffer の writable view から bump allocator を作る。返る Allocator は
buffer に tied で、生きている間 buffer を exclusive borrow に保つ。解放は
no-op、枯渇は `OutOfMemory`、メモリは全 owner と allocator の解放後
(同じ buffer に新しい allocator を作れる)か frame 終了で戻る。

checker に `fixed_buffer` という**名前の特別扱いはない**。「`&var` 引数を
取り `Allocator` を返す関数」という署名の形が tied allocator を定義し、
user が同じ形の wrapper を書いても同じ規則で検査される(原理 11)。

### 2. tie は 2 チャネル(ADR-0098 の規則の精密化)

- **view チャネル**: view / borrow 引数 → view / borrow 戻り値(従来通り)
- **allocator チャネル**: tied な Allocator 引数 → scalar 以外の戻り値

チャネルを分けるので、`read_file(io, allocator, path, limit) -> !String` の
String が `path`(view 引数)に tie されることはない。String は bytes を
copy するからで、規則が意味と一致する。tie のない allocator
(`page_allocator()`)からは何も継承せず、**既存コードの意味は一切変わらない**。

### 3. tied 値の制約

| 対象 | 規則 |
| --- | --- |
| tied Allocator | `let` 束縛必須。copy / alias / escape 不可。`Allocator` 引数へは貸せる |
| factory return | source が全て param に根ざすときだけ可(caller が引数から再導出) |
| tied allocator から作った owner | owner のまま(`deinit` 必須)+ frame escape 不可(return / field 格納 / move 拒否)。結果は `let` に直接束縛 |
| helper 経由 | `Allocator` 引数を持つ関数は無変更で書ける。callee は無制約、caller 側で結果が tie を継承 |

allocator tie が frame を越える唯一の経路は明示 `Allocator` 引数である。
原理 4(明示 allocator)の引数が、そのまま tie の宣言を兼ねる。

### 4. handle 実体化(ADR-0092 決定 2 の履行)

- handle は `ptr` 1 本。NULL = page(従来の malloc 経路)、非 NULL = buffer
  先頭に書いた bump header。分岐は runtime の `kizu_rt_alloc/realloc/free`
  の 1 箇所に閉じる(原理 9)。function pointer table は作らない(原理 2)
- container(Array / Arena / Map)の runtime 構造体が handle を 1 field
  保持する。SPEC §15.3「owner は deinit に必要なものを内部で保持する」の
  履行で、Kizu source からは見えない
- bump の realloc は末尾 allocation のみ grow-in-place(成長 loop が buffer
  を焼き尽くさないため)。それ以外は alloc + copy

### 5. checker は既存機構の拡張のみ

新しい追跡系は作らない。tied Allocator は view の機構(borrowTargets +
escape 検査)に乗り、owner の tie は同じ borrowTargets を owner binding に
持たせる(`allocTied`)。借用連鎖 buffer ← view ← allocator ← owner は
last-use release を fixpoint 化して葉から解ける。let に束縛されなかった
tie は pending として statement 終端で拒否する。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| unsafe 版 fixed_buffer | 現在の kizu には unsafe 漏れの無い経路しかなく、初の unmarked bleed になる(ADR-0089 の doctrine 違反) |
| `contract Allocator` 化 | ADR-0092 決定 1 を維持。tied 値としての opaque 扱いで足り、開放は不可逆 |
| generic / method な tied factory | recognizer が無く tie が黙って落ちるため宣言時に拒否。実需が出たら additive に開放(generic は ADR-0129 が開いた。method は閉じたまま) |
| 別 view 型・lifetime 注釈 | ADR-0098 で確立した構造的導出で足りる。新構文ゼロ |

## 影響

- `internal/native`: kizu_rt_* 分岐、container 構造体の allocator field、
  `mem_fixed_buffer` primitive、bump 実装
- `internal/llvm`: `kizu_{array,arena,map}_new` へ allocator operand を渡す
- `internal/ownership`: tied allocator の recognizer(既存 borrow recognizer
  の拡張)、`allocTied` owner、pending taint、release の fixpoint 化
- `internal/types`: 変更なし(全規則が flow 検査で、責務分界どおり ownership 側)
- SPEC §9(allocator チャネル)、§15.3(factory と tied 規則)

## 再評価条件

- `fixed_buffer` は失敗を返せない署名なので、header も入らない極小 buffer には
  「常に確保が失敗する allocator」を返す。生成時失敗と枯渇を区別したい実需が
  出たら `-> !Allocator` 化を検討する
- generic / method 形の tied factory の実需が出たら、recognizer を拡張して
  宣言時拒否を外す
- 第三の allocator 種(freestanding 供給、ADR-0092 決定 4)を入れる時、
  kizu_rt_* の分岐形を再確認する
- tied owner の struct 格納(taint の型伝播)の実需が出たら、escape 全面
  禁止を段階的に緩める(閉→開は additive、原理 8)
