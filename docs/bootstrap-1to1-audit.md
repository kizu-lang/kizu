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
| lexer | `tests/selfhost` compares token kind, literal, byte span, line, column | strong for covered files | broaden to all conformance parseable files |
| parser | self-host AST counts, declaration order, and function detail snapshots vs Go parser | weak | full AST node model and dump equality across all parseable conformance fixtures |
| resolver | self-host root module graph/import snapshot vs Go resolver for selected pass/fail packages | weak | full Kizu module graph resolver and graph snapshot equality |
| diagnostics | self-host diagnostic objects cover lexer, parser, and resolver subset spans vs Go facts | weak | broaden to checker diagnostics and all conformance failure classes |
| type | self-host return/call/std-mem type snapshots compare selected pass/fail cases vs Go checker | weak | broaden to full type environment and all checker diagnostic classes |
| ownership | Go ownership checker runs conformance; self-host owned-call move snapshot covers selected borrow/moved-value fixtures | weak | full Kizu move/borrow checker with memory-safety diagnostic equality |
| IR | self-host IR summary plus normalized function/block/terminator dump for selected pass/fail fixtures | weak | broaden to full instruction dump equality across conformance fixtures |
| backend | Go LLVM/WASM smoke; self-host target/status/function/string/entry fingerprints | weak | broaden to output fingerprints for all backend-supported conformance fixtures |
| cache | Go module-aware cache tests include std source hash; self-host snapshot declares Go-owned cache switch contract | medium | keep Go-owned until Kizu owns filesystem, hashing, module graph, and artifact layout |

## Required Issues

The remaining work should stay issue-driven. Each issue must include examples,
oracle tests, and acceptance evidence before closing.

1. Full parser AST oracle: #112
2. Kizu module resolver oracle: #113
3. Kizu diagnostic object oracle: #114
4. Kizu type checker subset oracle: #115
5. Kizu ownership/memory-safety oracle: #116
6. Normalized IR dump oracle: #117
7. Backend smoke fingerprint oracle: #118
8. Cache ownership and self-host switch decision: #119
9. 1:1 completion gate that fails if any phase lacks coverage: #111

## Non-Goals For The Audit

This audit does not lower the bar by treating normalized counts as full
compatibility. Counts are only temporary bootstrap evidence.

This audit does not allow hidden fallback from Kizu to Go in a phase declared as
Kizu-owned. Fallbacks must be explicit, documented, and tested as Go-owned.
