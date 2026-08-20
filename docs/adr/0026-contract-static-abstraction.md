# ADR-0026: contract / impl は静的な適合表明に限定する

Status: 採用

## 背景

Kizu には、複数の型に同じ method set を要求し、その意図を source に残す仕組みが
必要である。一方、抽象的な出力先を切り替える具体的な用途はまだなく、標準出力と
標準エラーには explicit `Io` を取る concrete API があれば足りる。

dynamic dispatch を持つには object representation、vtable、borrow provenance、
method call lowering を一体で定義する必要がある。型検査だけを先に置くと、check は
通るが build / run できない第二経路になり、Kizu の「実装と実行経路は 1 本」という
原理に反する。

## 決定

`contract` は required method signatures を宣言する。型は対応する method を
持てば structural に適合する。`impl Contract for Type;` は任意の適合表明であり、
書かれた場所で method set を検査するが、method body や dispatch table は持たない。

polymorphic な呼び出しは static generic を明示的に instantiate し、通常の method
call と同じ経路で monomorphize する。runtime dynamic dispatch のための専用型や
keyword は持たない。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| concrete な用途より先に runtime dynamic dispatch を実装する | vtable、object lifetime、backend lowering を増やすが、現在の std と CLI は concrete API と static generic で書ける |
| 型検査だけ残し、native lowering は後から足す | `check` だけ成功する実行不能な言語機能になり、実装経路が分岐する |
| user が raw pointer と function pointer で vtable を手書きする | boilerplate と unsafe provenance を各利用者に負わせ、レビュー対象を増やす |
| Go interface のように runtime interface conversion を暗黙に行う | allocation、変換、dispatch の制御フローが source から見えない |

## 影響

- contract は method set の名前付けと静的な適合検査だけを担う
- `impl Contract for Type;` は意図を残せるが、runtime representation を変えない
- generic call は concrete type ごとの既存 lowering を使い、第二 backend を作らない
- runtime polymorphism が必要になった場合は、具体的な用途と native 実行を同じ変更で完成させる
