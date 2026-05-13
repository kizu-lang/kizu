# ADR-0011: Phase 8 以降は IR、LLVM、性能評価、WASM、unsafe/C ABI の順に進める

Status: 採用

## 背景

Phase 6 までに interpreter、type checker、move checker、borrow checker、arena / handle が計画されている。
当初 `comptime` は Phase 7 に置いていたが、IR と backend を先に進める方針へ変更する。
その後は compiler backend に進む。

WASM / WASI も重要だが、ユーザー要望として最初の codegen target は LLVM IR を優先する。
また、ビルド時間とキャッシュサイズの評価は早期からインクリメンタルに改善できるようにしたい。

## 決定

Phase 8 以降は次の順にする。

```text
Phase 8: typed SSA IR
Phase 9: LLVM IR backend
Phase 10: build cache / why-rebuild
Phase 11: WASM / WASI backend
Phase 12: unsafe / C ABI
Phase 13: comptime
Phase 14: C header import
```

## 影響

- `comptime` は Phase 13 に移動する
- compiler backend の最初の target は LLVM IR にする
- WASM / WASI は LLVM の後に実装する
- unsafe / C ABI は backend ができた後に実装する
- C header import は後回しにする
