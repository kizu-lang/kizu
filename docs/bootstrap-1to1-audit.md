# Go/Kizu Compiler 1:1 Audit

This document is the completion checklist for replacing Go compiler phases with
Kizu compiler phases. It records what "1:1" means in concrete artifacts and
which parts are still missing.

## Completion Definition

Kizu compiler 1:1 with the Go compiler means:

- every Go compiler phase has a Kizu implementation for the same input shape
- every phase emits a checked-in oracle snapshot with the same normalized schema
- positive examples pass in both implementations
- negative examples fail in both implementations with matching diagnostic
  message substrings and spans
- module packages, std sources, and single-file sources are all covered
- IR and backend smoke outputs are compared, not only counted
- build cache keys include every input used by the compared phases
- any accepted difference is documented in an ADR before the switch

Passing `go test ./...` is necessary but not sufficient. The tests must prove
each row below.

## Current Evidence

| Phase | Current evidence | Coverage strength | Missing for 1:1 |
| --- | --- | --- | --- |
| lexer | `tests/selfhost` compares token kind, literal, byte span, line, column for all conformance manifest sources | strong | none |
| parser | self-host AST counts, declaration order, declaration detail, and full selected AST node dump equality across all parseable conformance manifest sources vs Go parser | strong | none |
| resolver | self-host module graph/module path/import snapshot vs Go resolver for every module conformance manifest package | strong | none |
| diagnostics | self-host diagnostic objects cover lexer, parser, resolver missing/visibility, all `examples/negative/*.kizu` failure fixtures, module visibility/missing-module failures, full positive non-module conformance diagnostic-clean coverage, static checker message/span snapshots, and runtime failure class snapshots vs Go facts | strong | none |
| type | self-host return/basic/control/variant and match/typed-error/unsafe/contract/Dyn/comptime/stdlib/concurrency/Io/fs/path/String/Map/Array resource/call/std-mem/task/channel/queue/thread/parallel boundary diagnostics plus local binding type environment snapshots for all parseable conformance manifest sources compare against Go facts | strong | none |
| ownership | self-host ownership snapshots cover positive conformance sources plus all type-checkable ownership diagnostic fixtures, including moved-value, double-move, assignment-move, copy-preserving channel send, channel/task ownership-transfer moves, deinit use-after-move, Arena/Handle provenance/escape, String view invalidation, Array append move/element-borrow conflicts, task completion, if branch move, borrow escape, field borrow, mutable borrow conflict, and move-while-borrowed fixtures vs Go checker | strong | none |
| IR | self-host IR summary plus normalized function/block/opcode/result/operand/immediate/terminator dump equality across all positive conformance manifest sources and selected lowerability failures vs Go IR facts | strong | none |
| backend | self-host target/status/function/string/instruction/const/call/entry plus LLVM/WASM output-line fingerprints across the backend-supported fixture matrix, including target-specific emitter failures, vs Go emitters | strong | none |
| cache | Go module-aware cache tests cover no-op rebuild, rebuild reason classes, std source hash, status, and prune; self-host snapshot matches the Go-owned cache switch contract, including required inputs and blocked Kizu-owned APIs | strong | none |

## Tracking Issues

Completed phase rows keep their issue references here as audit history.
Incomplete rows remain open and must include examples, oracle tests, and
acceptance evidence before closing.

1. Full parser AST oracle: #112 (complete)
2. Kizu module resolver oracle: #113 (complete)
3. Kizu diagnostic object oracle: #114 (complete)
4. Kizu type checker subset oracle: #115 (complete)
5. Kizu ownership/memory-safety oracle: #116 (complete)
6. Normalized IR dump oracle: #117 (complete)
7. Backend smoke fingerprint oracle: #118 (complete)
8. Cache ownership and self-host switch decision: #119 (complete)
9. 1:1 completion gate that fails if any phase lacks coverage: #111 (open)

## Completion Gate

`tests/bootstrap` checks that this audit cannot silently claim completion while
rows still have incomplete coverage. The normal test keeps the audit internally
consistent. The strict final gate is opt-in and must fail until every phase is
strong and has no missing work:

```sh
KIZU_REQUIRE_1TO1=1 go test ./tests/bootstrap
```

## Non-Goals For The Audit

This audit does not lower the bar by treating normalized counts as full
compatibility. Counts are only temporary bootstrap evidence.

This audit does not allow hidden fallback from Kizu to Go in a phase declared as
Kizu-owned. Fallbacks must be explicit, documented, and tested as Go-owned.
