# Phase 13: comptime

状態: 未着手

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

- [ ] `comptime` の最小構文を決める
- [ ] comptime value と runtime value の境界を定義する
- [ ] comptime expression の評価順序を定義する
- [ ] comptime function parameter の扱いを定義する
- [ ] comptime error の表示形式を定義する
- [ ] borrow checker と comptime の境界ルールを定義する
- [ ] `comptime` の examples を追加する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] comptime で整数定数を計算できる
- [ ] comptime で型または宣言を選択できる
- [ ] runtime borrow を comptime 結果として返そうとすると error になる
- [ ] comptime 実行中の error が読める

## 範囲外

- macro
- proc macro
- AST rewrite
- token stream manipulation
- build script
- arbitrary filesystem access
