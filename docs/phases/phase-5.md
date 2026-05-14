# Phase 5: Local borrow checker

状態: 完了

## 目的

明示 lifetime なしで、ローカル限定 borrow の基本ルールを検査する。

## TODO

- [x] `&T` parameter を検査する
- [x] `&mut T` parameter を検査する
- [x] borrow が値を move しないことを検査する
- [x] borrow 中の値を move できないことを検査する
- [x] borrow を関数から返せないことを検査する
- [x] borrow を struct field に保存できないことを検査する
- [x] borrow が lexical block の外へ escape できないことを検査する
- [x] 明示 lifetime annotation を syntax として受け付けない
- [x] mutable borrow conflict を検査する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] `examples/borrow.kizu` が通る
- [x] `examples/mutable_borrow.kizu` が通る
- [x] borrow escape の例が期待どおり error になる
- [x] borrow を返す関数が error になる
- [x] borrow field を持つ struct が error になる

## 範囲外

- lifetime annotation
- non-local borrow
- arena / handle provenance
- unsafe pointer proof

## 実装メモ

v0.1 の borrow syntax は `&T` と `&mut T` です。
同一 call 内では borrow 引数と move 引数の lifetime が重なるものとして扱い、
同じ値を borrow しながら move しようとすると error にします。
`&T` と `&mut T`、および `&mut T` 同士の overlap も error にします。
