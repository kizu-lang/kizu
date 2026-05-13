# 横断: ビルド性能とキャッシュ評価

状態: 未着手

## 目的

Kizu のコンパイル時間、ビルド時間、キャッシュサイズを早期から測定できる状態にする。

この文書は番号付き Phase とは独立した横断 TODO として管理する。

## TODO

- [ ] `docs/perf.md` を性能評価の正として運用する
- [ ] 初期 baseline を記録する形式を決める
- [ ] benchmark 用 examples を決める
- [ ] 測定スクリプトを追加する
- [ ] cold run / warm run の測定方法を決める
- [ ] cache size の測定方法を決める
- [ ] `kizu cache status` の出力形式を決める
- [ ] `kizu cache prune` の方針を決める
- [ ] `kizu why-rebuild` の出力形式を決める
- [ ] Phase 8 の `kizu ir` 測定を追加する
- [ ] Phase 9 の LLVM IR generation 測定を追加する
- [ ] CI で測る項目とローカルだけで測る項目を分ける

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] 現在の Go 実装について baseline を取れる
- [ ] 将来の `kizu build` に追加すべき測定項目が定義されている
- [ ] cache size の上限方針が ADR または SPEC に書かれている

## 範囲外

- 高度な最適化
- LLVM backend の実装
- WASM / WASI backend の実装
- 分散ビルド
