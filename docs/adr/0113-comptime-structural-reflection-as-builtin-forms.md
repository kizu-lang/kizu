# ADR-0113: structural reflection は comptime 専用型ではなく組み込みの式の形にする

Status: 採用(方針確定・実装は #1078 で続く)

Issue: #1078

## 背景

`std::json` の `encode<T>` / `decode<T>` を書くには、struct の field を型付きで
列挙し、各 field を borrow の規則に乗せて扱う必要がある。encode v1
(ADR-0112)は手書き streaming で、field の列挙は caller が綴っている。

#1078 の candidate shape は `std::meta` に comptime 専用の**型**を置いていた。

```kizu
pub struct Field<T> { }
pub fn fields<T>() -> FieldList<T>;
pub fn field<T, comptime f: Field<T>>(value: &T) -> &f.Type borrows value;
```

この形は Zig の `@typeInfo` を写したものだが、Kizu に持ち込むと 3 つ払う
ことになる。

1. `Field<T>` / `FieldList<T>` は `type` 値を運ぶので、SPEC §13 の
   「`type` 値は runtime local / field / union payload / collection element /
   return value として保持できない」を、「comptime 専用値を含む aggregate は
   自身も comptime 専用になる」へ広げる必要がある
2. `comptime f: Field<T>` は non-type static argument の一般化を要求する。
   SPEC §13 は `<...>` を static argument list として予約しつつ、整数と
   文字列の static argument を**受理しない**と決めている
3. `-> &f.Type borrows value` の `borrows` 節は ADR-0098 が削除済みで、
   今の言語に存在しない綴りである

一方で Kizu は full generics を実装しない(SPEC §7)。**ユーザーは generic 型を
定義できない**ので、reflection の dispatcher が知るべき「入れ物の種類」は
`Array` / `Map` / `?T` / `Box` / `Handle` / `String` という std の閉じた集合で
尽きる。Zig が union + switch という一般機構を要るのは型空間が開いている
からであり、閉じた型空間に同じ機構を持ち込むのは過剰である。

## 決定

`std::meta` は comptime 専用の**型**を持たない。`type<T>` と同じく、compiler が
直接解決する**式の形**だけを持つ。

### `comptime for`

compile-time list の反復を導入する。綴りは既存の `for`(SPEC §6.11)と同じ
capture 構文にする。同じ構文要素に 2 つ目の綴りを作らない。

```kizu
comptime for std::meta::public_fields<T>() |f| {
    // 展開された各反復は型検査済みの Kizu code
}
```

`comptime if` と同じく、これは token stream や AST の書き換えではない。
展開された各反復を、その `f` の束縛のもとで type / ownership / borrow check
する。

反復対象は v1 では `std::meta::public_fields<T>()` だけとする。整数 range は
既存の `for` が持っているので、comptime 版を並べる理由が現れるまで開かない。

### 束縛 `f` の位置

`f` は値ではない。書ける位置は `std::meta::*` の static 引数だけに限る。

```kizu
std::meta::field_name<T, f>()      // OK
std::meta::field_type<T, f>        // OK(型の位置)
std::meta::field<T, f>(value)      // OK
let g = f;                         // NG
use_field(f);                       // NG
comptime if f == other { }          // NG
```

閉→開は additive、開→閉は breaking(原理 8)。meta 値を関数へ渡して抽象化を
組む需要が現れたときに、first-class 化を検討する。

### `std::meta` の形

```text
std::meta::is_struct<T>()               -> bool     comptime-only
std::meta::is_optional<T>()             -> bool     comptime-only
std::meta::is_array<T>()                -> bool     comptime-only
std::meta::is_box<T>()                  -> bool     comptime-only
std::meta::is_map<T>()                  -> bool     comptime-only
std::meta::element<T>                              comptime-only 型の位置
std::meta::public_fields<T>()                      comptime-only list(comptime for 専用)
std::meta::field_name<T, f>()           -> []u8     comptime-only
std::meta::field_type<T, f>                        comptime-only 型の位置
std::meta::field<T, f>(value: &T)       -> &F
```

- `is_*` は `comptime if` の条件に書ける。そのため comptime expression の
  評価対象に、これらの組み込み形を加える。述語の集合が閉じているのは
  ユーザーが generic 型を宣言できないからであり、この前提が変わるときは
  集合を見直す(再評価条件)
- `field_name` の値は source の field 名を持つ `[]u8` literal である。
  static storage を指し、確保は起きない(原理 4)
- `field_type<T, f>` は型の位置に書ける。`encode_value<field_type<T, f>>(...)`
  のように、静的引数として渡って再帰できる
- `element<T>` は `?T` / `Array<T>` / `Box<T>` の中身の型を取り出す

### `field<T, f>(value)` は field path borrow そのもの

新しい borrow 意味論を持ち込まない。`std::meta::field<T, f>(value)` は
`&value.<f の名前>`(SPEC §9 の field path borrow)と同じものと定義する。
provenance は ADR-0098 の構造導出に従うので、`borrows` 節は書かない。

- 借用の追跡、衝突判定、`&var` の排他は既存規則がそのまま効く
- `&var` 版が要るときは `field_mut<T, f>(value: &var T) -> &var F` を同じ
  規則で足す(初版は read 側だけ)

### 対象

v1 は struct の `pub` field のみ、**source 宣言順**で列挙する。

- private field は module 境界を越えて読めない。名前も `public_fields` と
  明示して、後から広げるときに別名で開けるようにする
- enum tag / union variant は別 issue とする
- 列挙順は決定性のために source 順で固定する

### 変えないもの

SPEC §13 の「`type` 値は runtime local / field / union payload /
collection element / return value として保持できない」は**そのまま**である。
本 ADR はこの規則を緩めずに reflection を成立させることを目的とする。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `std::meta::info<T>()` が返す union を `comptime match` で分岐(Zig 形) | comptime 専用型と、その伝染規則(`type` 値を含む aggregate は comptime 専用)が要る。閉じた型空間には過剰で、開→閉が breaking |
| `Field<T>` / `FieldList<T>` を comptime 専用型として持つ | 同上。加えて non-type static argument の一般化を引き込む |
| 述語関数の組(`is_array<T>()` + `element<T>()`)だけで型の分解を表す | 本 ADR はこれを採る。ただし種類が増えるたび述語が増える点は認識しており、種類が std の閉じた集合であることが前提 |
| `comptime for f in list` の `in` 構文 | 既存 `for 0..3 \|i\|` と綴りが割れる(原理 6) |
| 整数 range の `comptime for` も同時に入れる | 実需がない。既存 `for` があり、閉じたまま出す(原理 8) |

## 影響

- SPEC §13 に `comptime for`、`std::meta` の形、`f` の位置規則を追加
- comptime expression の評価対象に `is_struct` / `is_optional` の組み込み形を追加
- `internal/types` / `internal/ownership`: instantiation 時に `comptime for` を
  展開し、各反復を検査する。`field<T, f>` は既存の field path borrow に落とす
- `std::json` に `encode<T>` を足せるようになる。対象は所有の木(値・struct・
  `Box`・`Array`・`?T`・`Map`)で、`Handle<T>` は共有参照のため compile error
- `partial<T>` と `decode<T>` は本 ADR の対象外(#1626)

## 実装が示した制約

- **optional field は今のところ walk できない。** `?T` は static 引数に
  書けない(ADR-0101)ので、`field_type<T, f>` が `?i64` に解決した瞬間、
  それを次の generic に渡せない。`is_optional` と `element<?T>` は実装
  してあるが、ADR-0101 が開くまで struct の optional field は列挙の先で
  止まる
- **再帰的な所有データ構造は今のところ作れない。** `Array<Box<Node>>` は
  Box の owner payload cleanup 規則で、`?Box<Node>` は optional owner field
  の規則で、それぞれ宣言時に拒否される。したがって reflection の再帰が
  循環することも今はない
- **型が無限に育つ generic 呼び出しはコンパイラが停止しない**(#1627)。
  本 ADR の前からある instantiation の穴で、`comptime for` は新たな経路を
  作っていない。ただし型を辿る generic を書く機会は増える

## 再評価条件

- meta 値を関数へ渡す抽象化(共通の field walker を std に置く等)の実需が
  現れたとき、`f` の first-class 化を検討する
- union / enum の reflection が要るとき、`public_fields` と同じ規則で
  variant 列挙を足す
- ユーザー定義 generic 型を言語に入れるとき、`is_*` / `element` の閉じた
  集合を見直す
