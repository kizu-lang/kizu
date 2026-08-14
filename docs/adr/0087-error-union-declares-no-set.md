# ADR-0087: `!T` は error set を宣言しない

Status: 採用

Supersedes: ADR-0086 の決定 3

## 背景

ADR-0086 の決定 3 は「`!T` の error set は関数本体から推論する」と決め、
「Zig の inferred error set と同じ規則である」と書いた。影響の一覧にも
「checker に error set の推論が入る」を挙げている。

入っていない。checker が持っているのは推論ではなく、受理規則 1 本である。

```go
// internal/types/checker.go
// An inferred `!T` accepts a member of any error set, ...
if errorType == "" && c.errorSets[string(got)] != nil {
    return true, nil
}
```

set を求める場所はどこにもない。`!T` は「あらゆる set の member を受け取る
error union」として動いている。Zig の語彙で言えば inferred error set ではなく
`anyerror!T` である。

この差を検出する conformance case は 1 件もない。本当に推論する実装と、
何でも受け取る実装は、既存の example では区別がつかない。区別がつくのは
呼び出し側が `!T` の error を網羅 `match` したときだけで、そう書いた例が
ないからである。つまり「推論する」という文言は、検査対象を 1 つも
持たないまま仕様本文に載っていた。

## 決定

`!T` は error set を宣言しない error union である。推論はしない。

- `!T` は、あらゆる declared set の member を受け取る
- `E!T` は、`E` の member だけを受け取る
- 受理は片方向である。`!T` は `E!T` を吸収するが、`E!T` は `!T` を受け取らない
- `!T` の error に対する網羅 `match` は書けない。網羅したい関数は `E!T` と宣言する

## 理由

1. **表現が変わらない。** SPEC.md は error 値を「大域一意な整数 1 個」と定め、
   set をまたぐ変換を持たない。set を推論しても lowering は 1 bit も変わらない。
2. **推論が買うものが既に手に入る。** 推論で得られるのは呼び出し側の網羅
   `match` だけで、それは `E!T` と宣言すれば今すぐ得られる。
3. **コストが方向と逆を向く。** 本物の推論は call graph 上の不動点計算になる。
   ADR-0066 は generics で推論を採らないと決めており、error set の推論は
   同じ種類の機構を別の場所に持ち込む。
4. **検査されない仕様は仕様ではない。** ADR-0025 が記録した失敗と同じ形で、
   「推論する」は checker rule すら持たない文言だった。

## 影響

- SPEC.md の 2 行が「推論される error set」から「set を宣言しない error union」
  に変わる
- `examples/error_set_inferred.kizu` を `examples/error_set_undeclared.kizu` に
  改名する。この example が示すのは推論ではなく、set を宣言しない `!T` が
  2 つの異なる set を吸収することである
- conformance の feature tag から `inference` が外れる
- checker の 3 箇所のコメントが、実装していない推論を名乗るのをやめる
- **動作は変わらない。** 通る example の数も、生成される IR も変わらない

## 不採用にした案

**本当に推論する** (ADR-0086 決定 3 の実装)。理由 1〜3 で採らない。
`!T` の error に対する網羅 `match` が言語機能として要求されたとき、
そのときの要求と一緒に再検討する。それまでは `E!T` が答えである。
