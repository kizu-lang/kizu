# Phase 20: developer CLI experience

状態: 完了

## 目的

Kizu 自体の CLI と開発体験を整える。

## 方針

- `kizu fmt <file>` は stable formatter output を stdout に出す
- Phase 20 の formatter は AST の compact representation を使う
- `kizu check` は type / ownership / borrow / arena などの静的検査
- `kizu lint` は将来、style や suspicious pattern を扱う
- `kizu test` は将来、Kizu source 内の test declaration を実行する

## TODO

- [x] `kizu fmt <file>` の範囲を決める
- [x] formatter の安定出力ルールを決める
- [x] `kizu test` の最小テスト構文を決める
- [x] `kizu lint` の役割を `check` と分ける
- [x] diagnostics format を整理する
- [x] examples / tests の運用を決める

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] 少なくとも1つの CLI が実装される、または仕様として明確化される
- [x] formatter / test / lint の責務が重複しすぎない

## CLI

```sh
kizu fmt <file>
```

## 今後の責務

```text
kizu fmt    stable source formatting
kizu check  semantic correctness
kizu lint   style and suspicious patterns
kizu test   test declaration discovery and execution
```

## 範囲外

- package manager
- IDE / LSP
- remote cache
- full test framework
