# ADR-0100: struct への view 捕捉と tie の構造的導出

Status: 採用

Issue: なし(tied 値統一の第 3 例。ADR-0098 / ADR-0099 の view 版一般化)

## 背景

struct は `[]u8` field を宣言できるが、実行時の local view を格納しようと
すると checker が一律に「borrowed value cannot escape」で拒否していた。
このため runtime データへの cursor / iterator / tokenizer struct が
書けなかった。加えて、view を格納**しない** struct を返す関数にも local
view を渡せない過剰拒否(field 型を見ない blanket)があり、さらに
`&var` out-parameter 経由で local view が frame を escape する
soundness hole が実在した(格納した struct を返すと dangling view になる)。

導出に必要な情報 — どの field が view 型か、どの引数が view か — は
struct 宣言と署名にすべて揃っている。fixed_buffer(ADR-0099)が
Allocator に対してやったことを、view を持てる struct に適用する。

## 決定

1. **view を持てる struct**(field を transitively 辿って `[]u8` を含む型)
   は、`let` / `var` 初期化位置でだけ local view を捕捉できる。struct
   literal の直接捕捉と、その型を返す関数呼び出しの両方が対象。binding
   は source tie を持ち、source は field 初期化子/view 引数の borrow-class
   root の保守的統合。別の field が owner でも対象から外さない。
2. **let 以外の文脈は従来どおり拒否する。** 代入形・inline 引数・return
   での捕捉は tie を記録する binding が無いため escape のまま。allocator
   の pend protocol は再利用しない(let 限定なら不要)。
3. **view lend の精密化。** view binding を貸せる条件を「戻り値の型が
   view を運べない(scalar / void / view を持てない struct)」に広げ、
   同時に「view を保持できる型の `&var` parameter が無い」を追加する。
   後者が out-parameter smuggle hole を塞ぐ。`&var []u8` は差し替え
   不可(ADR-0096)のため除外。
4. **owner field と view field は同じ struct に置ける。** owner-free な
   値は borrow class を使い、owner obligation を持つ値は通常の owner
   binding に source tie を追加する。後者は明示 `deinit` 義務を保ち、
   cleanup まで source を借用する。tied allocator 由来の owner が既に使う
   「owner obligation + borrow targets」と同じ表現なので、別の lifetime
   機構を増やさない。
5. **borrow-class 値の `[]u8` field 読みは tie を継ぐ。** let では同じ
   source への tie として認識し、move 文脈では escape として拒否する。
   parameter root の捕捉は自由な値のまま(parameter は frame より
   長生きし、呼び出し側が署名から tie を再導出する)。

## 却下した代替案

- **lifetime 注釈(Rust の `struct Iter<'a>`)**: 構文追加であり、署名と
  宣言から導出可能な情報の再記入になる(原理: 導出できるものは書かせない)。
- **callee 側での格納禁止**: `out.name = bytes` を callee で禁止すると
  static 代入など正当な形も落ちる。view の class を知るのは caller であり、
  call site で塞ぐのが正しい altitude。
- **非 let 文脈への pend protocol 拡張**: allocator と違い、view 引数の
  lend 自体が let 外では不要なので、機構を増やさず拒否で足りる。
- **owner + view aggregate を拒否する**: lexer / parser のように source view
  と作業用 owner container を同時に持つ型を分割するか、source 全体を複製
  させる。型が既に持つ 2 つの義務を checker が追えるため却下。
- **error union 戻り値・nested literal・method 戻り値の捕捉**: 認識器の
  対象を広げるだけの逐次拡張が可能だが、初版は最小に保つ。

## 再評価条件

- error union(`!BytesIter`)を返す factory、nested struct literal、
  receiver method からの捕捉が必要になったとき(認識器の対象拡張)。
- 汎用 `[]u8` 戻り値(`fn f(v: []u8) -> []u8` の error union 版など)の
  tie 導出が必要になったとき。
