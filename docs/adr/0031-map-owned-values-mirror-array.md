# ADR-0031: std::map::Map の owned 値対応は Array の owned 要素流儀を踏襲する

Status: 採用（方針確定・実装は #1157 go 依存撤去の後）

## 背景

selfhost compiler の backend は、型 / シンボル / 構造体情報を「テキスト fact 行 +
文字列 lookup」で扱う。`compiled_fact_lookup` の構造 fact 側は `ir_index::IrIndex` の
二分探索で緩和済みだが、将来 backend をインメモリの構造化型 / シンボルテーブルへ
寄せる選択肢を検討した（性能パリティ自体は SPEC により v0.1 スコープ外）。

その際の言語側の制約が `std::map::Map<[]u8, V>` の **値型 V に対する Copy 制約**である。
現在 checker（`internal/types/checker.go` `parseMapType`）は「v0.2 では Map の value 型は
copy でなければならない」を強制している。理由は `map.get(key) -> !V` が **値返し（= copy）**
で値を取り出すためで、owned 値を copy すると double-free になることを避ける消極的な縛りである。

一方 `std::array::Array<T>` は **それ自身は owned 型**だが、**要素 T は owned でよい**
（backend は `Array<MirStmt>` 等を実用）。Array は copy しない取り出し方として
`at(index) -> !&T borrows self` / `at_mut(index) -> !&var T borrows self`（借用）と
`pop() -> !T`（move-out）を備える。Map だけが「値は Copy 限定」かつ借用 / move-out
アクセサを欠く、という **Array との非対称**が残っている。

## 決定

`std::map::Map<K, V>` の owned 値対応は、**新しい言語概念を導入せず、Array<T> が既に
確立した owned 要素の流儀をそのまま踏襲する**。

重要な前提（誤解防止）:

- **Map<K, V> 自身は owned 型のままで変更しない**（SPEC 8 章「copy できない型」に `map` が
  含まれる）。Map ハンドルは move され、copy にはならない。
- 撤廃するのは **値の型 V に対する Copy 制約**だけである。これは Map を copyable にする変更
  ではなく、Map が **owned 値を所有できる**ようにする変更であり、向きはむしろ逆（所有範囲の拡大）。

安全性は「Map の値を一律 Copy に縛る」のをやめ、**アクセサごとに**担保する。

採用する API（Array に対応づく）:

```kizu
map.at(key: []u8) -> !&V borrows self        // 借用（copy せず参照）  ← array.at
map.at_mut(key: []u8) -> !&var V borrows self // 可変借用（in-place 更新） ← array.at_mut
map.remove(key: []u8) -> !V                    // move-out（所有権を呼び出し側へ移す） ← array.pop
map.get(key: []u8) -> !V                       // 値返し（copy）。Copy な V 専用     ← array.get
map.insert(key: []u8, value: V) -> !void       // move-in（owned 値を Map が所有）    ← array.append/set
map.deinit() -> void                           // 全 owned 値を cascade deinit       ← array.deinit
```

意味:

- owned 値は `at` / `at_mut`（借用）または `remove`（move-out）で扱う。`get` は Copy な V のときだけ使う。
- `borrows self` は ADR-0016 / SPEC のローカル借用機構をそのまま使う（lifetime 注釈なし。借用は
  receiver の借用に束縛される）。
- `insert` は owned 値を Map に move-in する。**既存キーへの insert は、退避される旧 owned 値を
  deinit してから上書きする**（「上書きは旧値を drop」）。
- `deinit` は Map が所有する全 owned 値（必要なら owned キー）を deinit する。Copy 値では no-op。
- `remove` は owned 値の所有権を呼び出し側へ移す。move 後の Map スロットは空になる。

## v0.x の制約

- **キー型は `[]u8` 据え置き**（checker の「key は []u8」制約は維持。owned キーは別 ADR で扱う）。
- ネストした owned 値（例 `Map<[]u8, Array<T>>`）は、Copy 制約がそれを masking していただけで
  ある可能性が高く、**Copy 制約撤廃の副作用として通る見込み**。別途の設計はせず、実装時に
  `parseType` と backend lowering で **検証**する（通らなければ別 slice）。
- `keys()` / entries イテレータ等の反復 API は、消費者が要求した時点で additive 追加する
  （本 ADR では先行実装しない）。

## 実装範囲（すべて array の写し）

- **Go ランタイム / checker**: `map_at` / `map_at_mut` / `map_remove` builtin を
  `internal/interp/interp.go` と `internal/native/build.go`（C ランタイム）と
  `internal/types/checker.go` の型規則に追加する（現状は `map_get` / `map_deinit` のみ）。
  `map_deinit` を値 cascade に拡張する。`parseMapType` の value-Copy 制約を撤廃し、Map 値アクセスに
  Array 要素と同じ ownership / borrow 規則（`at` は self に束縛された借用を返す、`get` / `remove` は
  copy / move、`deinit` は cascade）を適用する。
- **selfhost**: 自己ホスト化が当該経路に到達した段で、同じものを selfhost 側の checker / runtime に写す。

## シーケンス

- **方針: 本 ADR で確定。**
- **実装: #1157（go 依存撤去）の後**に行う。理由:
  - Map は go 撤去の経路上に**無い**（parser / AST mutator は Map を使わず Array / arena = NodeId のみ。
    確認済み）。Map 拡張を先行しても go 撤去は進まない。
  - owned-Map 値を消費する「構造化型テーブル」は go 撤去の後に来る作業で、かつ式位置の
    多フィールド struct-literal lowering（C1 / C2）を前提とする。それは go 撤去が供給する。
  - したがって順序は「go 撤去（C1 / C2 を獲得）→ Map owned 値実装 → 構造化テーブルへの移行」。

## 影響

- ADR-0016（no explicit lifetimes）/ SPEC のローカル借用 `borrows self` をそのまま再利用し、
  新しい借用機構を増やさない。
- ADR-0017（safe Kizu メモリ安全性）を `at`/`remove`/`deinit` の ownership 規則で支える
  （owned 値の copy による double-free を構造的に排除）。
- ADR-0020（arena / handle）と整合する（owned 値の所有 / move / borrow の扱いを揃える）。
- `Array<T>` と `Map<K, V>` が「owned 値を持てる owned コンテナ」として API・ownership 規則の
  両面で対称になる。
- 「賢いコードより単純なコードを優先」（AGENTS.md）に沿う。既存 Array パターンの踏襲であり、
  Map 専用の新概念を導入しない。
