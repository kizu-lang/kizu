# ADR-0090: owned な値への match は payload を取り出せる

Status: 採用

Issue: #1473

## 背景

match の payload binding は、scrutinee の所有と payload の型に関係なく一律
borrow だった(`internal/ownership/checker.go` の `defineMatchArmPayload` /
`checkMatchExprArmValue`)。そのため copyable scalar の包み直しが弾かれる。

```kizu
fn select_value(value: Value) -> Selection {
    return match value {
        Bool(selected) => Selection::Selected(selected),
        // error: borrow error: borrowed value `selected` cannot escape
        Error(kind) => Selection::Error(kind),
    };
}
```

`selected` は owned な `value` が持つ copyable な `bool` で、borrow ではない。
selfhost の comptime value contract 実装で踏んだ形であり、union payload ごと
包み直す形(エラー変換)も同じ理由で書けない。

判断の土台にした、現状の checker の 3 つの事実:

1. **union へ borrow は格納できない。** `Msg::Text(view)`(view は String からの
   borrow)は構築時に `borrowed value cannot escape` で拒否される。つまり union に
   入っている payload は provenance-free である。
2. **leak は compile error ではない。** deinit を持つ owner 値を deinit せず scope
   から捨てても `check: ok` になる。deinit は「source 上に見える契約」であり、
   checker が追跡する義務ではない。
3. **owner union の deinit dispatch は既に payload を move out している。**
   `ownerDeinitDispatch` の特例は「owned な値からの move out」の実例が既に
   言語内にあることを示す。

## 決定

payload binding の扱いを、payload の型クラスと scrutinee の所有で決める。

### 1. scalar payload は常に copy binding

bool / 整数 / float / enum / error set / copy capability の payload は、
scrutinee が owned でも borrow でも copy として束縛される。provenance を
持たない純粋な値の copy はどの文脈でも安全で、scrutinee には何も起きない。

### 2. 宣言された struct / union payload は owned scrutinee から move out できる

owned scrutinee は次の 2 つ。

* **owned な named local**(borrow でない ident)。move する arm が 1 つでも
  あれば、arm 本体と match 以降でその値全体を moved にする。borrow 中なら
  `cannot be moved while borrowed` で拒否する
* **call の temporary**。ただし borrow を返しうる呼び出し
  (arena.get、method call、`borrows` 付き関数)は除く。temporary は match で
  死ぬので consume の記帳は要らない

borrow、projection(field access)、上記以外の式への match では、aggregate
payload は従来通り borrow binding のまま。必要な形が現れた時に field-level の
move 追跡と合わせて再検討する。

これは Rust の by-value match と同じ意味論である。Rust の E0509(Drop 型からの
move out 禁止)は採らない。E0509 は自動 Drop を守るための規則で、Kizu には
自動 cleanup が存在しない(事実 2)。owner union に新しい特例は足さず、deinit
dispatch の既存規則(事実 3)はそのまま維持する。

### 3. view 型 payload は常に escape 禁止のまま

`[]T` / `ptr<>` / `&` / `?` / 型パラメータ / 未分類の型は、scrutinee の
所有に関わらず borrow binding のまま(arm 内で読むのは今まで通り自由)。

事実 1 により今日の union 内の `[]T` は provenance-free で、copy out しても
安全である。それでも禁止を維持する理由は**可逆性の非対称**にある。

- 禁止 → 許可は additive(禁止を外すだけで既存コードは全部通る)
- 許可 → 禁止は breaking(一度許した escape は取り消せない)

zero-copy(borrows-in-union)を将来入れるかは arena の性能検証(#549)待ちで
未決である。未決の前提に賭けず、自由度が残る側に倒す。

分類は安全側 default にする: 明示的に scalar または宣言 aggregate と判定できた
型だけ escape を許し、判定に漏れた型はすべて borrow に落ちる。将来の型追加で
穴は開かない(漏れは「厳しすぎる」方向にしか倒れない)。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| view 型 payload も copy out を許す | 今日は安全(事実 1)だが既成事実になり、borrows-in-union 導入時に breaking を背負う。必要とする形が現存しない |
| scalar だけ直す最小修正(whitelist 2 行) | union payload の包み直しが弾かれたまま残り、selfhost 再構築で確実に再発する |
| 真の provenance 追跡(Rust lifetime 相当) | シンプル・明示的の公理と衝突する。#538 も「局所的な構文追加で終わるとは限らない」と警告済み |
| `[]u8` を static / dynamic の 2 種に分ける | 型が増え、ユーザーに区別を課す。上 2 案の悪いところ取り |

## 影響

- `internal/ownership`: `defineMatchArmPayload` / `checkMatchExprArmValue` の
  分類、scrutinee consume の一般化(`consumeOwnerUnionReceiver` の仕組みを流用)
- SPEC §6.8 に payload binding の所有規則を追記
- 診断文言は現行を維持する(view と borrowed scrutinee は今まで通り
  `cannot escape`、move 後の使用は `moved value was used`)
- 正例は `tests/behavior/`、負例は `examples/negative/` に置く

## 再評価条件

- borrows-in-union / zero-copy を導入する時、決定 3 を見直す(#549 の結果が前提)
- ident でない scrutinee(field access 等)からの move out は、必要な API が
  現れた時に検討する
- `?T` payload の copy out は、`?i64` のような scalar optional が実際に union
  payload に現れた時に scalar 側へ広げるか判断する
