# ADR-0032: owned コンテナの複製（clone）は明示。owned 要素は per-type copy 関数で書く

Status: 採用（方針確定・Copy 要素 clone builtin を足す場合のみ実装が要り、それも #1157 後で可）

## 背景

Kizu の owned 型（`std::array::Array<T>`、`std::map::Map<K, V>`、`std::string::String`、
`std::mem::Box<T>` 等）は copy できず move される（SPEC 8 章 / ADR-0017）。値の複製が必要な
場面で、これらをどう複製するかの方針を固定する。

Kizu には複製を汎用化する以下の機構が **無い**:

- `Clone` trait / trait bound（`T: Clone`）— full generics は v0.x 非対象。
- クロージャ / 第一級 fn 値（SPEC: closure はローカルを capture できず runtime data に保持できない）
  → `clone_with(要素複製関数)` のような高階 API も書けない。
- copy constructor / 自動 deep clone フック（copy 型はビット複製、フック無し）。

一方で selfhost compiler は既に owned 構造を複製している。その実体は **明示的な手書き
per-type copy 関数**である（例: `selfhost/src/backend/compiled_mir.kizu` の
`copy_call_arg -> MirCallArg` / `copy_call_args -> !Array<MirCallArg>` /
`copy_expr_inst -> !MirExprInst` / `copy_expr_insts` / `copy_cond_operands`、
`compiled_mir_types.kizu` の `copy_cached_call_arg_types` 等）。`String` にも `clone` は無く、
複製は新 `String` を作り `append_bytes(other.as_bytes())` で行う。

つまり「汎用 `.clone()` は無く、per-type の明示 copy 関数で複製する」が既に事実上の標準である。
本 ADR はこれを方針として追認し、Copy 要素に限った例外を定める。

## 決定

複製（clone）は **常に明示**とし、要素 / 値の所有性で 2 層に分ける。

1. **暗黙 clone / `Clone` trait / owned 要素コンテナの汎用 deep clone は提供しない。**
   owned 要素の複製は「要素ごとの複製ロジック」を要するが、Kizu には trait bound も
   クロージャも無く、汎用 `Array<T>.clone()`（owned T 対応）は v0.x の言語機能では
   **表現不能**。これを無理に builtin 化しない。

2. **owned 要素コンテナ**（`Array<String>`、`Array<MirExprInst>`、`Map<[]u8, OwnedV>` 等）:
   **per-type の明示 copy 関数**で複製する。各要素を要素自身の `copy_<Type>` で deep-copy し、
   新コンテナへ rebuild する（`compiled_mir.kizu` の現行イディオムを正とする）。

   ```kizu
   pub fn copy_expr_insts(src: &std::array::Array<MirExprInst>)
       -> !std::array::Array<MirExprInst> {
       var out = std::array::Array<MirExprInst>(std::mem::page_allocator());
       var i = 0;
       while i < src.len() {
           try out.append(try copy_expr_inst(try src.at(i)));
           i = i + 1;
       }
       return out;
   }
   ```

3. **Copy 要素 / 値コンテナ**（`Array<i64>`、`Array<NodeId>`、`Array<[]u8>`、
   `Map<[]u8, i64>` 等）: 必要なら `clone()` を **Copy 要素限定の builtin** として足してよい
   （バッファのビット複製＝安全・安価）。checker は ADR-0031 と同じ Copy 判定で
   「要素 / 値型が Copy のときだけ」と縛る。これは任意の利便機能であり、明示 rebuild でも代替可。

   ```kizu
   array.clone() -> Array<T>        // T が Copy のときだけ。バッファをビット複製
   map.clone() -> Map<[]u8, V>      // V が Copy のときだけ
   ```

## 意味と制約

- **`[]u8` 要素の clone は view コピー**である。`[]u8` は所有しない Copy ビュー（ptr+len）なので、
  `Array<[]u8>.clone()` は各ビューを複製するだけで、**背後のバイト列は複製しない**（両配列の
  `[]u8` が同じ backing を指す）。`[]u8` は所有者でないため double-free にならず、これは正しい
  セマンティクス（`copy_cached_call_arg_types` の既存挙動と一致）。背後バイトまで複製したい場合は
  明示的に新バッファへコピーする。
- Copy 要素 clone は **shallow=deep** が一致する（Copy 要素はそれ以上の所有を持たない）。
- owned 要素 clone は **per-type 関数の責務**であり、ネスト owned（`Array<Array<U>>` 等）は
  各層の copy 関数を組み合わせて表現する。
- 自動 derive は行わない。複製が要る owned 型ごとに copy 関数を明示的に書く。

## 実装

- owned 要素 clone は **言語 / runtime 変更を要しない**（既存の `Array.at` / `append` /
  per-type copy 関数で書ける）。今すぐ既存パターンとして使える。
- Copy 要素 `clone()` builtin を足す場合のみ runtime / checker 変更が要る
  （`array_clone` / `map_clone` builtin + Copy 要素制約）。これは任意で、必要になった時点・
  かつ #1157（go 依存撤去）の後で構わない。Map 側は ADR-0031 の owned 値対応と同じ Copy 判定を再利用。

## 影響

- ADR-0018（明示 return / 暗黙を避ける）と整合: 複製は常に明示呼び出し、暗黙 clone は無い。
- ADR-0017（safe Kizu メモリ安全性）を支える: owned 値の暗黙ビット複製による double-free を排除。
- ADR-0031（Map owned 値対応）と一対: Map が owned 値を持てる一方、その複製は per-type 明示。
- copy / move モデルと一致: Copy → 複製可 / owned → move か明示 rebuild。clone も同じ二分。
- 「賢いコードより単純なコードを優先」（AGENTS.md）: 新概念ゼロ。既存 `copy_*` イディオムの追認。
