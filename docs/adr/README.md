# ADR

ADR は Architecture Decision Record の略です。

Kizu の設計判断は、仕様本文とは別にこのディレクトリで管理します。

## 目的

- なぜその設計にしたかを残す
- 未確定の方針と採用済みの方針を分ける
- 後から仕様を変更するときに判断の履歴を追えるようにする

## ステータス

- `提案`: 方針として有力だが、まだ実装または仕様確定していない
- `採用`: 現時点の方針として採用する
- `置換`: 別の ADR に置き換えられた
- `却下`: 検討したが採用しない

## 一覧

- [ADR-0001: ADR で設計判断を管理する](0001-use-adr.md)
- [ADR-0002: Kizu は低レベル寄りのシステムプログラミング言語を目指す](0002-system-programming-language.md)
- [ADR-0003: 基本型は Zig 寄りの小文字表記にする](0003-lowercase-primitive-types.md)
- [ADR-0004: ローカル束縛は let / var を使う](0004-let-var-bindings.md)
- [ADR-0005: 実装フェーズは Markdown の TODO と受け入れ条件で管理する](0005-phase-documents.md)
- [ADR-0006: comptime は採用候補とし、macro は採用しない](0006-comptime-without-macros.md)
- [ADR-0007: unsafe は低レベル操作の明示境界にする](0007-unsafe-boundary.md)
- [ADR-0008: C 親和性は ABI / FFI / layout / pointer で確保する](0008-c-interop.md)
- [ADR-0009: compiler backend の前に Kizu IR を導入する](0009-kizu-ir-before-backends.md)
- [ADR-0010: ビルド時間とキャッシュサイズの評価方法を早期に確立する](0010-build-performance-evaluation.md)
- [ADR-0011: Phase 8 以降は IR、LLVM、性能評価、WASM、unsafe/C ABI の順に進める](0011-phase-order-to-llvm.md)
- [ADR-0012: 低レベル型セットは Zig 寄りに広めに持つ](0012-low-level-type-set.md)
- [ADR-0013: raw pointer は ptr<T> / ptr<const T>、nullable pointer は ?ptr<T> にする](0013-pointer-and-nullability.md)
- [ADR-0014: Kizu IR は typed SSA IR にする](0014-typed-ssa-ir.md)
- [ADR-0015: string は標準ライブラリ管理に寄せる](0015-string-and-stdlib.md)
- [ADR-0016: 明示 lifetime annotation は採用しない](0016-no-explicit-lifetimes.md)
- [ADR-0017: safe Kizu のメモリ安全性を保証する](0017-safe-kizu-memory-safety.md)
- [ADR-0018: 戻り値は explicit return にする](0018-explicit-return-no-rust-tail-expression.md)
- [ADR-0019: Code comments are written in English and required for packages and functions](0019-code-comments-in-english.md)
- [ADR-0020: arena / handle は v0 専用構文として扱う](0020-arena-handle-syntax.md)
- [ADR-0021: ローカルビルドキャッシュは上限付きにする](0021-local-build-cache.md)
- [ADR-0022: Phase 11 の WASM backend は WAT 生成から始める](0022-wasm-wat-backend.md)
- [ADR-0023: low-level type conversion は明示 cast に限定する](0023-low-level-type-conversion.md)
- [ADR-0024: C ABI layout と native linking は明示指定に限定する](0024-c-abi-layout-and-linking.md)
- [ADR-0025: async は Io / TaskGroup で明示する](0025-async-io-taskgroup-policy.md)
- [ADR-0026: contract / satisfy / Dyn は明示的な抽象化として扱う](0026-contract-satisfy-dyn-policy.md)
- [ADR-0027: v0.1 は interpreter-first language core とする](0027-v0-1-interpreter-first.md)
- [ADR-0028: enum は Zig/C 寄りの tag enum にする](0028-zig-style-enum-and-tagged-union.md)
- [ADR-0029: active work は GitHub Issues で管理する](0029-issue-based-work-tracking.md)
- [ADR-0030: エラー処理は Zig 風の !T に寄せる](0030-zig-style-error-union.md)
- [ADR-0031: 幅が曖昧な int を廃止する](0031-remove-ambiguous-int.md)
- [ADR-0032: v0.1 はメモリ安全性監査を release gate にする](0032-v0-1-memory-safety-release-gate.md)
- [ADR-0033: borrow syntax は &T / &mut T にする](0033-reference-borrow-syntax.md)
- [ADR-0034: dereference と field assignment は Zig 寄りにする](0034-zig-style-deref-and-field-assignment.md)
- [ADR-0035: v0.1 loop control は while / for / labeled branch に限定する](0035-v0-1-loop-control.md)
- [ADR-0036: statement semicolon を必須にする](0036-require-statement-semicolons.md)
- [ADR-0037: v0.1 で if expression を採用する](0037-if-expression-in-v0-1.md)
- [ADR-0038: namespace lookup は `::` に限定する](0038-explicit-namespace-separator.md)
- [ADR-0039: Io runtime は Zig 0.16 寄りの選択式 interface にする](0039-zig-style-io-runtime-interface.md)
- [ADR-0040: v0.1 data parallelism は range と partition に限定する](0040-data-parallel-collection-policy.md)
- [ADR-0041: std::mem and allocator boundary](0041-std-mem-allocator-boundary.md)
- [ADR-0042: std::array::Array<T> owned buffer](0042-std-array-owned-buffer.md)
- [ADR-0043: std::string::String owned buffer](0043-std-string-owned-buffer.md)
- [ADR-0044: std::map::Map<K, V> symbol table map](0044-std-map-symbol-table.md)
- [ADR-0045: std::testing minimal API](0045-std-testing-minimal-api.md)
- [ADR-0046: std::fs / std::path minimal API](0046-std-fs-path-minimal-api.md)
- [ADR-0047: std::io / std::process minimal API](0047-std-io-process-minimal-api.md)
- [ADR-0048: module import and manifest policy](0048-module-import-manifest.md)
- [ADR-0049: module graph and name resolution](0049-module-graph-name-resolution.md)
- [ADR-0050: visibility and diagnostics](0050-visibility-diagnostics.md)
- [ADR-0051: compiler outputs, build cache, and bootstrap criteria](0051-compiler-outputs-cache-bootstrap.md)
- [ADR-0052: Zig-style native build policy](0052-zig-style-native-build-policy.md)
- [ADR-0053: checked index and slice syntax](0053-checked-index-and-slice-syntax.md)
- [ADR-0054: self-host migration readiness gate](0054-self-host-readiness-gate.md)
