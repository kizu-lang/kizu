# ADR-0019: Code comments are written in English and required for packages and functions

Status: 採用

## 背景

Kizu compiler は Go で実装する。
Phase が進むにつれて parser、checker、IR、backend、cache などの責務が増える。

コメント規約が曖昧なままだと、実装意図や境界条件がコードから読み取りにくくなる。

## 決定

Go code comments are written in English.

Required comments:

- package comments
- command comments for `package main`
- function comments
- method comments

コメントは実装の逐語説明ではなく、責務、前提、境界条件、失敗条件を説明する。

## 影響

- Go package には `doc.go` または package declaration 直前の package comment を置く
- every function and method must have an immediately preceding comment
- pre-commit で package / function comment の欠落を検出する
- 日本語の設計文書は許可するが、Go code comments は英語に統一する
