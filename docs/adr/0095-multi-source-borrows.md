# ADR-0095: `borrows a, b` — 複数 source の保守的統合

Status: 採用

Issue: #538

Supersedes: ADR-0060 の単一 source 制限

## 背景

ADR-0060 は borrowed return の由来を `borrows <source>` で明示すると決めたが、
source を 1 個に制限した。そのため「2 つの view を受けて片方を返す」関数が
書けない。

```kizu
fn pick(a: []u8, b: []u8, take_first: bool) -> []u8 borrows a {
    if take_first { return a; }
    return b;  // error: 書けない
}
```

#538 はこの制限を「実需が出るまで」延期していたが、単一 source 制限は
原理から導かれた線ではなく、複数 source の意味論を決めていなかっただけの
恣意的な線である。systems programming language の制約として違和感が残る。

lifetime 変数(ADR-0059 で採用 → ADR-0060 で破棄)を持たない言語での
複数 source には、実は形が 1 つしかない。他言語の同型物が示している:

| 言語 | 形 |
| --- | --- |
| C++ (Clang) | `[[clang::lifetimebound]]` を parameter 側に付ける(複数可、warning 止まり) |
| D | `return scope` parameter(DIP1000、checker あり) |
| Swift | Span 系の lifetime dependency(`@lifetime(borrow self)`、実験段階) |
| C# | `Span<T>` は注釈なしで「全 span 引数由来」と保守的に仮定、除外だけ `scoped` |
| Rust / Mojo | lifetime 変数 / origin — ADR-0060 で拒否した方向 |

C# の検査意味論が本 ADR の「保守的統合」と同型で、健全性の実運用前例になる。

## 決定

`borrows` の source を comma 区切りの列挙に拡張し、意味を保守的統合と定める。

- **callee 側**: すべての return 値が、列挙した source のどれかに由来する。
  列挙にない source へ tie され得る値を返すと error(candidate を名指しする)
- **caller 側**: checker はどの source が選ばれたかを追跡しない。戻り値が
  生きている間、列挙された**全 source** を borrow 中として扱う
- 重複列挙は error。mutable 戻り値では全 source が exclusive borrow になる
  ため、同じ値を 2 つの source に渡すと aliasing error
- 過剰列挙(body が返さない source の列挙)は許可する。契約を広めに宣言した
  だけで、caller がより保守的になる方向は嘘にならない

「v は実際どちら由来か」を追跡して選ばれなかった source を解放するには
lifetime 変数が要り、それは拒否済み。保守的統合は Rust の単一 lifetime に
統合した場合と同じ表現力であり、これ以上は変数化しない限り上がらない。
**この拡張で `borrows` の設計は閉じる** — 「次は 3 source」の梯子は残らない
(任意個の comma 列挙で完成)。#538 が警戒した「局所的な構文追加で終わらない
可能性」への答えは「保守的統合なら局所追加で正しく終わる」である。

区切りは `|` でなく comma にする。`|` は Kizu では capture 記号
(`for 0..3 |i|`)であり、二重の意味になる。また「a または b」の or 読みは
「checker が由来を追跡している」という持っていない精度を約束してしまう。
`borrows a, b` は「この view は a, b を borrow している」— caller 側で実際に
enforce される契約そのもの — と読め、書いてあることと起きることが一致する。

語は `borrows` を維持する。発動する規則が SPEC §9 の borrow 規則そのもの
なので同じ機構に同じ名前(原理 7)、grep で全 borrowed-return 境界を列挙
できる(原理 3)。source は parameter 名で、定義があり LSP で辿れる。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `borrows a \| b` | `\|` は capture 記号と衝突。or 読みは由来追跡の精度を偽る |
| `from a, b` へ改名 | grep 不能な汎用語。borrow 規則が適用される含意が消え、同一機構への第二語彙になる |
| lifetime 変数 / origin | ADR-0060 で拒否済み。選ばれなかった source の解放はこれでしか得られないが、lifetime programming の再導入になる |
| C# 式の推論(全引数を暗黙 source 化) | boundary の契約が source に見えなくなり、明示性の公理と矛盾 |
| 現状維持(単一 source) | 制限が原理から導かれていない。回避には API の分割か caller 側分岐の強制が要る |

## 影響

- SPEC §9: 単一 source 制限の段落を複数 source の定義に置換
- `internal/ast`: `ReturnBorrow string` → `ReturnBorrows []string`
- `internal/parser`: `borrows` 後の comma ループ
- `internal/types`: 宣言 policy(重複・未知 source)、return site の検査を
  「候補すべてが列挙済み、かつ少なくとも 1 つに tie」へ一般化。binding の
  provenance 記録を単一 source から source 集合に変更。列挙外 candidate の
  error は原因を名指しする形に改善
- `internal/ownership`: caller 側で列挙された全 source arg を borrow 化
  (binding の borrow target を複数化)。`&x` 形式の inline 引数の source 解決
  も修正(単一 source 時代からの潜在ギャップ)
- 既存の単一 source 使用(std 13 箇所)は無変更で有効。追加は additive

## 再評価条件

- 「選ばれなかった source を早期解放したい」実需が繰り返し出た場合。
  ただしその解決は lifetime 変数の再導入であり、ADR-0060 の決定ごと
  再評価することになる
