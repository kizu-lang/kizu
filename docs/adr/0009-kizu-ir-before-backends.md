# ADR-0009: compiler backend の前に Kizu IR を導入する

Status: 採用

## 背景

将来、WASM / WASI / LLVM / native backend を検討する。
AST から各 backend へ直接変換すると、型、制御フロー、所有権、一時値の扱いが backend ごとに散らばる。

## 決定

compiler backend の前に Kizu IR を導入する。

想定パイプライン:

```text
.kizu source
  -> tokens
  -> AST
  -> checked AST
  -> Kizu IR
  -> backend
```

Kizu IR は次を表現する。

- 明示された型
- 関数
- 基本ブロック
- 分岐
- ループ
- ローカル変数
- 一時値
- 関数呼び出し
- return
- move / copy 済みの情報
- borrow 検査後の安全な参照情報

IR の具体形は [ADR-0014](0014-typed-ssa-ir.md) に従い、typed SSA IR とする。

## 影響

- Phase 8 以降に IR を追加する
- LLVM、WASM / WASI は IR の後に扱う
- `kizu ir <file>` のような dump command を将来追加してよい
