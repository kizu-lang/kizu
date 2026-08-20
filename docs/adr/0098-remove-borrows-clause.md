# ADR-0098: `borrows` 節を削除し、契約を構造的に導出する

Status: 採用

Issue: なし(#538 起点の tied 値探索の帰結)

Supersedes: ADR-0060 の節構文、ADR-0095 の節構文(意味論は既定として存続)

## 背景

ADR-0060 は borrowed return の由来を `borrows <source>` で明示させ、
ADR-0095 は複数 source(保守的統合)へ拡張した。その直後の tied 値の設計
探索で、次が判明した。

戻り値の tie は署名から**構造的に導出できる**: 「tie を運べる型の引数
すべてに tied。ただし戻り値の型が構造的に運べる分だけ」。この既定を
現行の全使用箇所に当てると:

| 既存の節 | 構造的既定 | 一致 |
| --- | --- | --- |
| `basename(path: []u8) -> []u8 borrows path` | i64 等は tie 不能、源は path のみ | ✓ |
| `Array.at(self, i: i64) -> !&T borrows self` | {self} | ✓ |
| `pick(a, b, f: bool) -> []u8 borrows a, b` | {a, b} = 保守的統合 | ✓ |

std 13 箇所 + 複数 source 例のすべてで、節は**既定が導く内容の書き写し**
だった。導出可能な情報を人間に書かせる構文は儀式である(ADR-0091 が
転送 deinit の宣言に下したのと同じ判定)。

## 決定

`borrows` 節を言語から削除する。契約は次の 1 規則に置き換える:

**戻り値は、その型が構造的に運べる範囲で、tie を運べる全引数に tied。**

- tie を運べない型(scalar、bool、enum)の戻り値は常に自由
- view / borrow を運べる型の戻り値は、view / borrow 引数(と receiver)の
  保守的統合に tied(ADR-0095 の意味論がそのまま既定になる)
- `&var T` 返しの源は `&var` 引数に限る(`&T` からは作れないため)
- 契約は署名だけから導出でき、body は見ない(署名検査・build 速度の公理は
  無傷)

節の削除で消えるもの: 節の parse、宣言 policy(「&T 返しは borrows 必須」
を含む)、return-site の宣言照合。呼び出し側の由来導出と borrow 活性化は
宣言列挙から構造的列挙に置き換わり、保守的統合・複数 target borrow 化
(ADR-0095 で実装した機構)はそのまま既定の実装として存続する。

## 失うものと可逆性

union より狭い契約(2 つの view を受けて片方の view しか返さない関数で、
もう片方を caller が早く解放する)は書けなくなる。過剰保守は常に安全側で、
回避は「生かしておく」で可能。狭め節の再導入は optional 構文の追加であり
**additive**(原理 8: 今消して、実害の実例が出たら戻す)。再評価条件は
「`&var` 引数との tie 連鎖で std の実 API が書けない事例」が出た時。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 節の維持(任意化) | 全使用箇所が既定の書き写しで、情報量ゼロの儀式。節あり/なしの 2 モード読みが残る |
| binding 側 `borrows` 断言 | 安全性への寄与ゼロの任意注釈。canonical form を割る。可視化は LSP と診断の仕事 |
| 型への tie 付与 | 境界で lifetime parameter 化するか、精度を捨てた qualifier になり全型構成子と絡む(ADR-0016/0059/0060 で却下済みの方向) |

## 影響

- SPEC §9: 節の定義を構造的既定の 1 規則に置換。「borrowed view の戻り値は
  `borrows` で由来を明示する」の公理行を削除
- `internal/parser` / `internal/ast`: 節の parse と `ReturnBorrows` を削除。
  `borrows` の出現は parse error
- `internal/types`: 宣言 policy と return-site 照合を削除。caller 側の由来
  導出を構造的列挙に変更
- `internal/ownership`: `&T` 返し呼び出しの borrow 活性化を構造的列挙に変更
- std の節 15 箇所と examples / tests の節を削除(挙動は不変)

## 再評価条件

- 狭め契約の実需(上記)が std / 実プログラムで繰り返し出た場合、
  optional な狭め節を additive に再導入する
