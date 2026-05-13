# Phase 13: comptime

状態: 完了

## 目的

Zig に近い低レベル指向のため、限定的な `comptime` を導入する。

`comptime` は macro ではなく、コンパイル時に評価される通常の Kizu コードとして扱う。

## 方針

- `comptime` は型検査と所有権検査の対象にする
- `comptime` の実行結果は AST 文字列置換ではなく、型付きの値または宣言として扱う
- runtime borrow が comptime 境界を越えて escape することは禁止する
- comptime 値は runtime の所有権状態を直接変更できない
- macro は採用しない
- proc macro は採用しない
- token stream / AST rewrite API は提供しない

## TODO

- [x] `comptime` の最小構文を決める
- [x] comptime value と runtime value の境界を定義する
- [x] comptime expression の評価順序を定義する
- [x] comptime function parameter の扱いを定義する
- [x] comptime error の表示形式を定義する
- [x] borrow checker と comptime の境界ルールを定義する
- [x] `comptime` の examples を追加する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] comptime で整数定数を計算できる
- [x] comptime で型または宣言を選択できる
- [x] runtime borrow を comptime 結果として返そうとすると error になる
- [x] comptime 実行中の error が読める

## v0.1 構文

comptime expression:

```kizu
let size = comptime 4 * 1024
```

comptime parameter:

```kizu
fn sized(comptime n: int) -> int {
    return n
}
```

comptime branch:

```kizu
comptime if 1 + 1 == 2 {
    print(sized(comptime 8))
} else {
    print(0)
}
```

## 実装メモ

- `comptime <expr>` は整数、真偽値、文字列、単項演算、二項演算だけを評価する
- `comptime if` は選ばれた branch だけを type check / ownership check / lowering する
- `comptime` parameter への runtime value の受け渡しは禁止する
- runtime local value を `comptime` expression から参照すると `comptime error` にする
- borrow / move / ownership の通常ルールは `comptime` でも無効化しない
- v0.1 では型そのものを値として返す type-level comptime は未実装

「型または宣言を選択」は v0.1 では `comptime if` による branch / call path selection として扱う。
型値や top-level declaration generation は macro 的な広がりが大きいため、今後の phase で別途判断する。

## 範囲外

- macro
- proc macro
- AST rewrite
- token stream manipulation
- build script
- arbitrary filesystem access
