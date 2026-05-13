# 横断: ビルド性能とキャッシュ評価

状態: 完了

## 目的

Kizu のコンパイル時間、ビルド時間、キャッシュサイズを早期から測定できる状態にする。

この文書は番号付き Phase とは独立した横断 TODO として管理する。

## TODO

- [x] `docs/perf.md` を性能評価の正として運用する
- [x] 初期 baseline を記録する形式を決める
- [x] benchmark 用 examples を決める
- [x] 測定スクリプトを追加する
- [x] cold run / warm run の測定方法を決める
- [x] cache size の測定方法を決める
- [x] `kizu cache status` の出力形式を決める
- [x] `kizu cache prune` の方針を決める
- [x] `kizu why-rebuild` の出力形式を決める
- [x] Phase 8 の `kizu ir` 測定を追加する
- [x] Phase 9 の LLVM IR generation 測定を追加する
- [x] Phase 11 の WASM generation 測定を追加する
- [x] CI で測る項目とローカルだけで測る項目を分ける

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] 現在の Go 実装について baseline を取れる
- [x] 将来の `kizu build` に追加すべき測定項目が定義されている
- [x] cache size の上限方針が ADR または SPEC に書かれている

## 実装メモ

初期 baseline は `scripts/measure-baseline.sh` で測る。
cache 固有の cold / warm / no-op / small edit は `scripts/measure-cache.sh` で測る。
WASI smoke は `scripts/run-wasi-smoke.sh` で測る。

cache size の上限方針は ADR-0021 に残す。

## 範囲外

- 高度な最適化
- LLVM backend の実装
- 分散ビルド
